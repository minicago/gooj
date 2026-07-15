package sql_service

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// RejudgeSubmission resets a submission to "queued" so it will be picked up and
// re-judged. Old test results are removed first to avoid duplicates.
func RejudgeSubmission(id uint) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("submission_id = ?", id).Delete(&TestResult{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&Submission{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status":       "queued",
			"score":        0,
			"disqualified": false,
			"updated_at":   time.Now(),
		}).Error; err != nil {
			return err
		}
		return nil
	})
}

// CancelEvaluation marks a queued/running submission as "cancelled" so the judge
// ignores it. A running job's eventual result is discarded by UpdateSubmissionResult.
func CancelEvaluation(id uint) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	return db.Model(&Submission{}).Where("id = ?", id).
		Where("status IN ?", []string{"queued", "running"}).
		Update("status", "cancelled").Error
}

// CancelScore disqualifies a submission: its score is set to 0, its status is
// changed to "score_cancelled" (shown greyed out as "成绩取消" in the UI), and the
// disqualified flag is set so the record is kept but not counted in rankings.
func CancelScore(id uint) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	return db.Model(&Submission{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":       "score_cancelled",
		"score":        0,
		"disqualified": true,
		"updated_at":   time.Now(),
	}).Error
}

// RestoreScore undoes CancelScore: clears the disqualified flag and resets the
// status so the submission is no longer shown as "成绩取消". Note the score was
// zeroed at cancel time, so the operator should re-judge (or the real score will
// remain 0) to restore it.
func RestoreScore(id uint) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	return db.Model(&Submission{}).Where("id = ?", id).Updates(map[string]interface{}{
		"status":       "not accepted",
		"disqualified": false,
		"updated_at":   time.Now(),
	}).Error
}

// BatchSubmissionAction applies an administrative action to a set of submissions.
// Supported actions: "rejudge", "cancel_eval", "cancel_score", "restore_score".
// It returns the number of submissions successfully affected.
func BatchSubmissionAction(action string, ids []uint) (int, error) {
	if db == nil {
		return 0, errors.New("db not initialized")
	}
	affected := 0
	for _, id := range ids {
		var err error
		switch action {
		case "rejudge":
			err = RejudgeSubmission(id)
		case "cancel_eval":
			err = CancelEvaluation(id)
		case "cancel_score":
			err = CancelScore(id)
		case "restore_score":
			err = RestoreScore(id)
		default:
			return affected, errors.New("unknown action: " + action)
		}
		if err == nil {
			affected++
		}
	}
	return affected, nil
}

// GetSubmissionsByProblem returns all submissions (including code) for a problem,
// used by the similarity check.
func GetSubmissionsByProblem(problemID uint) ([]Submission, error) {
	if db == nil {
		return nil, errors.New("db not initialized")
	}
	var subs []Submission
	if err := db.Where("problem_id = ?", problemID).Order("id asc").Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

// SaveSimilarityRecords replaces the stored similarity records for a problem.
func SaveSimilarityRecords(records []SimilarityRecord) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	return db.Create(&records).Error
}

// DeleteSimilarityByProblem removes all similarity records for a problem.
func DeleteSimilarityByProblem(problemID uint) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	return db.Where("problem_id = ?", problemID).Delete(&SimilarityRecord{}).Error
}

// GetSimilarityByProblem returns similarity records for a problem ordered by
// descending similarity.
func GetSimilarityByProblem(problemID uint) ([]SimilarityRecord, error) {
	if db == nil {
		return nil, errors.New("db not initialized")
	}
	var records []SimilarityRecord
	if err := db.Where("problem_id = ?", problemID).Order("similarity desc").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

// RejudgeProblem re-queues every non-cancelled submission of a problem so it will
// be re-judged. It returns the number of submissions that would be (or were)
// affected.
//
// When confirm is false it performs NO writes and just returns the count, so the
// caller can warn the operator about the load before committing. A problem can
// have a very large number of submissions; re-queuing them all at once can flood
// the judge workers and starve other submissions, so the confirm step (plus a UI
// warning) is the safety guard against accidentally hammering the judge machines.
func RejudgeProblem(problemID uint, confirm bool) (int, error) {
	if db == nil {
		return 0, errors.New("db not initialized")
	}
	var subs []Submission
	// Exclude both "cancelled" (eval cancelled) and "score_cancelled" (disqualified)
	// submissions so a whole-problem rejudge never silently restores a disqualified
	// score.
	if err := db.Where("problem_id = ? AND status NOT IN ?", problemID, []string{"cancelled", "score_cancelled"}).Find(&subs).Error; err != nil {
		return 0, err
	}
	if !confirm {
		return len(subs), nil
	}
	ids := make([]uint, len(subs))
	for i, s := range subs {
		ids[i] = s.ID
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Where("submission_id IN ?", ids).Delete(&TestResult{}).Error; err != nil {
			return err
		}
		return tx.Model(&Submission{}).Where("id IN ?", ids).Updates(map[string]interface{}{
			"status":       "queued",
			"score":        0,
			"disqualified": false,
			"updated_at":   time.Now(),
		}).Error
	})
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}
