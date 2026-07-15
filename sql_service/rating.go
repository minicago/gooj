package sql_service

import (
	"errors"
	"log"
	"math"
	"sort"
	"time"

	"github.com/minicago/gooj/config"
	"gorm.io/gorm"
)

const (
	// K-factor determines rating volatility (higher = more change per game)
	DefaultKFactor = 32
	// Initial rating for new users
	InitialRating = 1500
)

// CalculateExpectedScore returns expected score based on Elo formula
func CalculateExpectedScore(ratingA, ratingB int) float64 {
	return 1.0 / (1.0 + math.Pow(10, float64(ratingB-ratingA)/400))
}

// CalculateNewRating calculates new rating after a contest
func CalculateNewRating(currentRating, opponentAvgRating int, score float64, kFactor int) int {
	expected := CalculateExpectedScore(currentRating, opponentAvgRating)
	newRating := currentRating + int(float64(kFactor)*(score-expected))
	return newRating
}

// CalculateContestRating calculates and stores rating changes for all participants in a contest
// This function uses a transaction to ensure rating settlement only happens once, even if steps fail
func CalculateContestRating(contestID uint) error {
	if db == nil {
		return errors.New("db not initialized")
	}

	// Get contest details
	contest, err := GetContestByID(contestID)
	if err != nil {
		return err
	}

	// Check if contest has ended
	if time.Now().Before(contest.EndAt) {
		return errors.New("contest has not ended yet")
	}

	// Check if rating has already been settled (using the flag for atomic check-and-set)
	if contest.RatingSettled {
		return errors.New("rating already settled for this contest")
	}

	// Atomically mark as settled to prevent concurrent or retry attempts
	// Use raw SQL update to ensure it's atomic even if transaction fails later
	if err := db.Model(&Contest{}).Where("id = ? AND rating_settled = ?", contestID, false).Update("rating_settled", true).Error; err != nil {
		return errors.New("failed to mark contest as settled")
	}

	// Now proceed with the actual calculation (wrapped in transaction)
	// If anything fails from here, rating_settled stays true so no retry
	tx := db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get leaderboard with scores
	leaderboard, err := GetContestLeaderboard(contestID)
	if err != nil {
		tx.Rollback()
		return err
	}

	if len(leaderboard) == 0 {
		tx.Rollback()
		return errors.New("no participants in contest")
	}

	// Calculate average rating of all participants
	totalRating := 0
	for _, row := range leaderboard {
		totalRating += row.Rating
	}
	avgRating := totalRating / len(leaderboard)

	// Highest total score, used for the score-based component of the rating blend.
	maxTotal := 0
	for _, row := range leaderboard {
		if row.Total > maxTotal {
			maxTotal = row.Total
		}
	}

	// Calculate ranks considering ties (same total score = same rank)
	// Sort by total descending, username ascending for stable ordering
	sortedLeaderboard := make([]ContestRankingRow, len(leaderboard))
	copy(sortedLeaderboard, leaderboard)
	sort.Slice(sortedLeaderboard, func(i, j int) bool {
		if sortedLeaderboard[i].Total != sortedLeaderboard[j].Total {
			return sortedLeaderboard[i].Total > sortedLeaderboard[j].Total
		}
		return sortedLeaderboard[i].Username < sortedLeaderboard[j].Username
	})

	// Assign ranks with tie handling
	rankings := make(map[string]int)
	for i := range sortedLeaderboard {
		username := sortedLeaderboard[i].Username
		if i == 0 {
			rankings[username] = 1
		} else {
			// Check if tied with previous
			if sortedLeaderboard[i].Total == sortedLeaderboard[i-1].Total {
				rankings[username] = rankings[sortedLeaderboard[i-1].Username]
			} else {
				rankings[username] = i + 1
			}
		}
	}

	// Customizable rating weights (set by admin). rankWeight blends the rank-based
	// performance (1st place ~ 1.0) with a score-based performance (total / maxTotal).
	rankWeight := config.GetRatingRankWeight()
	kFactor := config.GetRatingKFactor()

	// Calculate and store rating changes
	histories := make([]ContestRatingHistory, 0, len(leaderboard))
	for _, row := range leaderboard {
		username := row.Username

		// Get current user rating
		var user User
		if err := tx.Where("username = ?", username).First(&user).Error; err != nil {
			continue
		}

		ratingBefore := user.Rating
		rank := rankings[username]

		// Rank-based score ratio (0 to 1): rank 1 gets 1.0, last place gets 0.0.
		scoreRatioByRank := 1.0
		if len(leaderboard) > 1 {
			scoreRatioByRank = 1.0 - (float64(rank-1) / float64(len(leaderboard)-1))
		}
		// Score-based score ratio (0 to 1): own total / best total in contest.
		scoreRatioByScore := 0.0
		if maxTotal > 0 {
			scoreRatioByScore = float64(row.Total) / float64(maxTotal)
		}
		// Blend the two components using the admin-configured rank weight.
		scoreRatio := rankWeight*scoreRatioByRank + (1-rankWeight)*scoreRatioByScore

		// Calculate new rating using Elo-based formula with the configured K-factor
		ratingAfter := CalculateNewRating(ratingBefore, avgRating, scoreRatio, kFactor)

		history := ContestRatingHistory{
			Username:     username,
			ContestID:    contestID,
			ContestName:  contest.Title,
			Rank:         rank,
			TotalScore:   row.Total,
			RatingBefore: ratingBefore,
			RatingAfter:  ratingAfter,
			RatingChange: ratingAfter - ratingBefore,
			CreatedAt:    time.Now(),
		}
		histories = append(histories, history)

		// Update user's rating in user table
		if err := tx.Model(&User{}).Where("username = ?", username).Update("rating", ratingAfter).Error; err != nil {
			tx.Rollback()
			return err
		}
	}

	// Batch insert all rating histories
	if err := tx.Create(&histories).Error; err != nil {
		tx.Rollback()
		return err
	}

	// Reveal contest problem test results to non-editors
	if err := revealContestProblemTestsTx(tx, contest.ID); err != nil {
		log.Printf("Warning: failed to reveal problem tests for contest %d: %v", contest.ID, err)
		// Don't fail the transaction for this warning
	}

	return tx.Commit().Error
}

// revealContestProblemTestsTx sets TestVisible=true for all problems in a contest (within transaction)
func revealContestProblemTestsTx(tx *gorm.DB, contestID uint) error {
	// Get contest with its problems
	var contest Contest
	if err := tx.Preload("Problems").First(&contest, contestID).Error; err != nil {
		return err
	}

	if len(contest.Problems) == 0 {
		return nil
	}

	// Build problem IDs
	problemIDs := make([]uint, len(contest.Problems))
	for i, p := range contest.Problems {
		problemIDs[i] = p.ID
	}

	// Update all problems in the contest to reveal test results
	return tx.Model(&Problem{}).Where("id IN ?", problemIDs).Update("test_visible", true).Error
}

// revealContestProblemTests sets TestVisible=true for all problems in a contest
func revealContestProblemTests(contestID uint) error {
	if db == nil {
		return errors.New("db not initialized")
	}

	// Get contest with its problems
	contest, err := GetContestByID(contestID)
	if err != nil {
		return err
	}

	if len(contest.Problems) == 0 {
		return nil
	}

	// Build problem IDs
	problemIDs := make([]uint, len(contest.Problems))
	for i, p := range contest.Problems {
		problemIDs[i] = p.ID
	}

	// Update all problems in the contest to reveal test results
	return db.Model(&Problem{}).Where("id IN ?", problemIDs).Update("test_visible", true).Error
}

// GetUserRatingHistory returns all rating changes for a user
func GetUserRatingHistory(username string) ([]ContestRatingHistory, error) {
	if db == nil {
		return nil, errors.New("db not initialized")
	}

	var histories []ContestRatingHistory
	if err := db.Where("username = ?", username).Order("created_at desc").Find(&histories).Error; err != nil {
		return nil, err
	}
	return histories, nil
}

// GetEndedContestsWithoutRating returns contests that have ended but rating not yet settled
func GetEndedContestsWithoutRating() ([]Contest, error) {
	if db == nil {
		return nil, errors.New("db not initialized")
	}

	now := time.Now()
	var contests []Contest

	// Find contests not yet settled (rating_settled is a bool, safe to filter in
	// SQL), then filter "ended" in Go. We must not compare end_at in SQL: contest
	// times are stored in UTC while now serializes with the local offset, and
	// SQLite's TEXT string comparison across timezones is wrong (a running contest
	// can look ended and vice-versa).
	var candidates []Contest
	if err := db.Where("rating_settled = ?", false).Find(&candidates).Error; err != nil {
		return nil, err
	}
	for _, c := range candidates {
		if c.EndAt.Before(now) { // contest has ended
			contests = append(contests, c)
		}
	}

	return contests, nil
}

// GetStartedContestsWithoutReveal returns contests that have started but problems are not yet visible
func GetStartedContestsWithoutReveal() ([]Contest, error) {
	if db == nil {
		return nil, errors.New("db not initialized")
	}

	now := time.Now()
	var contests []Contest

	// Find contests that still have hidden problems (the EXISTS check is timezone
	// independent, so it stays in SQL), then filter "has started" in Go. We must
	// not compare start_at in SQL because contest times are stored in UTC while now
	// serializes with the local offset, and SQLite's TEXT string comparison across
	// timezones is wrong.
	var candidates []Contest
	if err := db.
		Where("EXISTS (SELECT 1 FROM contest_problems cp JOIN problems p ON p.id = cp.problem_id WHERE cp.contest_id = contests.id AND p.problem_visible = false)").
		Find(&candidates).Error; err != nil {
		return nil, err
	}
	for _, c := range candidates {
		if !now.Before(c.StartAt) { // start_at <= now
			contests = append(contests, c)
		}
	}

	return contests, nil
}

// RecomputeAllRatings resets every user's rating to the initial value and replays
// all ended contests in chronological order, applying the *current* rating weights
// from config. Ratings accumulate across contests, so changing the weights for one
// contest is only consistent if the whole chain is rebuilt.
func RecomputeAllRatings() error {
	if db == nil {
		return errors.New("db not initialized")
	}

	// Reset all users to the initial rating and wipe existing rating history.
	if err := db.Model(&User{}).Where("1 = 1").Update("rating", InitialRating).Error; err != nil {
		return err
	}
	if err := db.Where("1 = 1").Delete(&ContestRatingHistory{}).Error; err != nil {
		return err
	}
	// Allow every ended contest to be settled again.
	if err := db.Model(&Contest{}).Where("1 = 1").Update("rating_settled", false).Error; err != nil {
		return err
	}

	// Replay all ended contests in chronological order. We fetch all contests
	// ordered by end_at (an intra-column ordering that is consistent even as TEXT)
	// and filter "ended" in Go with a timezone-safe time.Time comparison, because
	// contest times are stored in UTC while now serializes with the local offset.
	now := time.Now()
	var allContests []Contest
	if err := db.Order("end_at asc, id asc").Find(&allContests).Error; err != nil {
		return err
	}
	var contests []Contest
	for _, c := range allContests {
		if c.EndAt.Before(now) {
			contests = append(contests, c)
		}
	}
	for _, c := range contests {
		if err := CalculateContestRating(c.ID); err != nil {
			// A single failed contest must not abort the whole rebuild; log and skip.
			log.Printf("RecomputeAllRatings: skipped contest %d: %v", c.ID, err)
		}
	}
	return nil
}

// RecalculateContestRating recomputes rating for a specific contest using the
// current weights. Because ratings accumulate across contests, this rebuilds the
// entire rating chain (equivalent to RecomputeAllRatings) so the result stays
// correct and consistent with the other contests.
func RecalculateContestRating(contestID uint) error {
	return RecomputeAllRatings()
}
