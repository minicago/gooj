package sql_service

import (
	"errors"
	"sort"
	"time"
)

// ContestRankingRow describes one contestant's aggregate score in a contest.
type ContestRankingRow struct {
	Username  string       `json:"username"`
	GroupName string       `json:"group_name"`
	Rating    int          `json:"rating"`
	Scores    map[uint]int `json:"scores"`
	Total     int          `json:"total"`
}

// ListContests returns all contest definitions sorted by start time.
func ListContests() ([]Contest, error) {
	if db == nil {
		return nil, errors.New("db not initialized")
	}
	var contests []Contest
	if err := db.Preload("Groups").Order("start_at asc").Find(&contests).Error; err != nil {
		return nil, err
	}
	return contests, nil
}

// GetContestByID returns a contest by ID.
func GetContestByID(id uint) (Contest, error) {
	if db == nil {
		return Contest{}, errors.New("db not initialized")
	}
	var contest Contest
	if err := db.Preload("Groups").Preload("Problems").First(&contest, id).Error; err != nil {
		return Contest{}, err
	}
	return contest, nil
}

// CreateContest stores a contest and its linked problem IDs.
func CreateContest(title, description, createdBy string, startAt, endAt time.Time, groupNames []string, problemIDs []uint) (Contest, error) {
	if db == nil {
		return Contest{}, errors.New("db not initialized")
	}
	contest := Contest{Title: title, Description: description, CreatedBy: createdBy, StartAt: startAt, EndAt: endAt}
	contest.Groups = []Group{}
	for _, groupName := range groupNames {
		var group Group
		if err := db.Where("name = ?", groupName).First(&group).Error; err != nil {
			return Contest{}, err
		}
		contest.Groups = append(contest.Groups, group)
	}
	contest.Problems = []Problem{}
	for _, problemID := range problemIDs {
		var problem Problem
		if err := db.First(&problem, problemID).Error; err != nil {
			return Contest{}, err
		}
		contest.Problems = append(contest.Problems, problem)
	}
	if err := db.Create(&contest).Error; err != nil {
		return Contest{}, err
	}

	return contest, nil
}

// DeleteContest removes a contest together with its problem link records.
func DeleteContest(id uint) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	var contest Contest
	if err := db.First(&contest, id).Error; err != nil {
		return err
	}
	// Use GORM's Association API to clear many-to-many relationships
	if err := db.Model(&contest).Association("Problems").Clear(); err != nil {
		return err
	}
	if err := db.Model(&contest).Association("Groups").Clear(); err != nil {
		return err
	}
	// Also delete any rating history for this contest
	if err := db.Exec("DELETE FROM contest_rating_histories WHERE contest_id = ?", id).Error; err != nil {
		return err
	}
	return db.Delete(&contest).Error
}

// RevealContestProblems sets ProblemVisible=true for all problems in a contest.
// Called when a contest starts to make its problems visible.
func RevealContestProblems(contestID uint) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	var contest Contest
	if err := db.Preload("Problems").First(&contest, contestID).Error; err != nil {
		return err
	}
	for _, problem := range contest.Problems {
		if err := db.Model(&problem).Update("problem_visible", true).Error; err != nil {
			return err
		}
	}
	return nil
}

// ContestProblemInfo contains only the necessary problem info for contest listing
type ContestProblemInfo struct {
	ID          uint   `json:"id"`
	Title       string `json:"title"`
	TestsCount  int    `json:"tests_count"`
	TimeLimitMs int    `json:"time_limit_ms"`
	MemLimitMB  int    `json:"mem_limit_mb"`
}

// ListContestProblems returns all problems linked to a contest (without Description).
func ListContestProblems(contestID uint) ([]ContestProblemInfo, error) {
	if db == nil {
		return nil, errors.New("db not initialized")
	}
	// Query the contest_problems junction table directly to get problem IDs
	var problemIDs []uint
	if err := db.Table("contest_problems").
		Where("contest_id = ?", contestID).
		Pluck("problem_id", &problemIDs).Error; err != nil {
		return nil, err
	}
	if len(problemIDs) == 0 {
		return []ContestProblemInfo{}, nil
	}
	// Only select fields needed (avoid selecting large Description field)
	var problems []Problem
	if err := db.Select("id, title, tests_count, time_limit_ms, mem_limit_mb").
		Where("id IN ?", problemIDs).Find(&problems).Error; err != nil {
		return nil, err
	}
	result := make([]ContestProblemInfo, len(problems))
	for i, p := range problems {
		result[i] = ContestProblemInfo{
			ID:          p.ID,
			Title:       p.Title,
			TestsCount:  p.TestsCount,
			TimeLimitMs: p.TimeLimitMs,
			MemLimitMB:  p.MemLimitMB,
		}
	}
	return result, nil
}

// GetContestLeaderboard computes a simple scoreboard for the problems in a contest.
func GetContestLeaderboard(contestID uint) ([]ContestRankingRow, error) {
	if db == nil {
		return nil, errors.New("db not initialized")
	}
	contest, err := GetContestByID(contestID)
	if err != nil {
		return nil, err
	}

	var problemIDs []uint

	for _, problem := range contest.Problems {
		problemIDs = append(problemIDs, problem.ID)
	}

	if err != nil {
		return nil, err
	}
	if len(problemIDs) == 0 {
		return []ContestRankingRow{}, nil
	}

	var submissions []Submission
	// Order by created_at desc to get the most recent submission first for each user+problem.
	//
	// IMPORTANT: we deliberately do NOT filter the contest time window in SQL.
	// SQLite stores datetimes as TEXT, and contest times are persisted in UTC
	// (e.g. "2026-07-15 05:08:00+00:00") while submission.created_at is persisted
	// with the local offset (e.g. "2026-07-15 13:04:12+08:00"). A raw SQL string
	// comparison would compare "13:.." > "05:.." and silently drop every in-contest
	// submission, producing an empty leaderboard. Instead we fetch the candidates
	// and filter the window in Go using timezone-safe time.Time comparisons.
	if err := db.Where("problem_id IN ?", problemIDs).
		Order("username asc, problem_id asc, created_at desc").
		Find(&submissions).Error; err != nil {
		return nil, err
	}

	// For each user+problem, keep only the most recent submission's score
	lastSubmissionByUserProblem := make(map[string]map[uint]Submission)
	for _, submission := range submissions {
		if submission.Username == "" {
			continue
		}
		// Timezone-safe contest window check (StartAt <= createdAt <= EndAt).
		if submission.CreatedAt.Before(contest.StartAt) || submission.CreatedAt.After(contest.EndAt) {
			continue
		}
		if _, ok := lastSubmissionByUserProblem[submission.Username]; !ok {
			lastSubmissionByUserProblem[submission.Username] = make(map[uint]Submission)
		}
		// Since we order by created_at desc, the first one we see for each user+problem is the latest
		if _, exists := lastSubmissionByUserProblem[submission.Username][submission.ProblemID]; !exists {
			lastSubmissionByUserProblem[submission.Username][submission.ProblemID] = submission
		}
	}

	leaderboard := make(map[string]*ContestRankingRow)
	for username, problemSubmissions := range lastSubmissionByUserProblem {
		row := &ContestRankingRow{
			Username: username,
			Scores:   make(map[uint]int),
		}
		for problemID, submission := range problemSubmissions {
			row.Scores[problemID] = submission.Score
			row.Total += submission.Score
		}
		leaderboard[username] = row
	}

	result := make([]ContestRankingRow, 0, len(leaderboard))
	for username, row := range leaderboard {
		// enrich with user rating and group
		var user User
		if err := db.Where("username = ?", username).First(&user).Error; err == nil {
			row.Rating = user.Rating
			row.GroupName = user.GroupName
		}
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Total != result[j].Total {
			return result[i].Total > result[j].Total
		}
		return result[i].Username < result[j].Username
	})
	return result, nil
}

// func listContestProblemIDs(contestID uint) ([]uint, error) {
// 	if db == nil {
// 		return nil, errors.New("db not initialized")
// 	}
// 	var ids []uint
// 	if err := db.Model(&ContestProblem{}).Where("contest_id = ?", contestID).Order("problem_id asc").Pluck("problem_id", &ids).Error; err != nil {
// 		return nil, err
// 	}
// 	return ids, nil
// }
