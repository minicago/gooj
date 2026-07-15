package sql_service

import "time"

// User model represents a user in the system
type User struct {
	ID         uint   `gorm:"primaryKey"`
	Username   string `gorm:"uniqueIndex;size:128"`
	Password   string
	Role       string `gorm:"size:32;default:'user'"`                // user, admin, teacher
	Group      Group  `gorm:"foreignKey:GroupName;references:Name;"` // User group
	GroupName  string
	Rating     int    `gorm:"default:1500"` // User rating, default 1500
	Bio        string `gorm:"type:text"`    // User biography in markdown format
	CreatedAt  time.Time
	CreatedBy  string     `gorm:"size:128"`      // Username of the creator
	Approved   bool       `gorm:"default:false"` // Whether the user is approved by creator
	ApprovedAt *time.Time // When the user was approved
	ApprovedBy string     `gorm:"size:128"` // Who approved the user
}

type Group struct {
	ID              uint   `gorm:"primaryKey"`
	Name            string `gorm:"uniqueIndex;size:128"`
	EditPermission  bool
	UserPermission  bool
	GroupPermission bool
	CreatedAt       time.Time
	CreatedBy       string `gorm:"size:128"` // Username of the creator
}

// Permission model represents a structured permission type
// type Permission struct {
// 	ID   uint   `gorm:"primaryKey"`
// 	Name string `gorm:"uniqueIndex;size:128"` // Permission name, e.g., edit_problems
// }

// Submission model represents a code submission
type Submission struct {
	ID           uint         `json:"id" gorm:"primaryKey"`
	Username     string       `json:"username" gorm:"index;size:128"`
	ProblemID    uint         `json:"problem_id" gorm:"index"`
	Code         string       `json:"code" gorm:"type:text"`
	Status       string       `json:"status" gorm:"size:32"`             // queued, running, accepted, not accepted, compile_error, runtime_error, time_limit_exceeded, memory_limit_exceeded, internal_error, cancelled
	Score        int          `json:"score"`                             // Total score obtained
	MaxMemoryKB  int          `json:"max_memory_kb"`                     // Maximum memory usage in KB
	MaxTimeMs    int          `json:"max_time_ms"`                       // Maximum time usage in ms
	Disqualified bool         `json:"disqualified" gorm:"default:false"` // score cancelled / disqualified (record kept, score not counted)
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	TestResults  []TestResult `json:"test_results" gorm:"foreignKey:SubmissionID"`
}

// SimilarityRecord stores a pairwise code-similarity result between two submissions
// of the same problem. Used by the plagiarism / similarity check feature.
type SimilarityRecord struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	ProblemID   uint      `json:"problem_id" gorm:"index"`
	SubmissionA uint      `json:"submission_a"`
	SubmissionB uint      `json:"submission_b"`
	UserA       string    `json:"user_a" gorm:"size:128"`
	UserB       string    `json:"user_b" gorm:"size:128"`
	Similarity  float64   `json:"similarity"` // Jaccard similarity in [0,1]
	CreatedAt   time.Time `json:"created_at"`
}

// TestResult model represents the result of a test case
type TestResult struct {
	ID           uint   `json:"id" gorm:"primaryKey"`
	SubmissionID uint   `json:"submission_id" gorm:"index"`
	TestIndex    int    `json:"test_index" gorm:"column:test_index"`
	Passed       bool   `json:"passed"`
	Output       string `json:"output" gorm:"type:text"`
	TimeMs       int    `json:"time_ms"`
	MemoryKB     int    `json:"memory_kb"`
	Status       string `json:"status" gorm:"size:32"`
	Score        int    `json:"score"` // Score for this test case
}

// Problem model represents a coding problem
type Problem struct {
	ID uint `json:"id" gorm:"primaryKey"`
	// Name        string `gorm:"uniqueIndex;size:128"`
	Title          string `json:"title" gorm:"size:256"`
	Description    string `json:"description" gorm:"type:text"`
	TestsCount     int    `json:"tests_count"`
	TimeLimitMs    int    `json:"time_limit_ms"`
	MemLimitMB     int    `json:"mem_limit_mb"`
	ProblemVisible bool   `json:"problem_visible" gorm:"default:false"` // If false, non-editors cannot view the problem until contest starts
	TestVisible    bool   `json:"test_visible" gorm:"default:false"`    // If false, non-editors cannot see evaluation info (test results, scores, etc.)
}

// Contest represents a contest with an associated problem set and a leaderboard.
type Contest struct {
	ID uint `json:"id" gorm:"primaryKey"`
	// Name        string    `gorm:"uniqueIndex;size:128"`
	Title       string    `json:"title" gorm:"size:256"`
	Description string    `json:"description" gorm:"type:text"`
	StartAt     time.Time `json:"start_at" gorm:"index"`
	EndAt       time.Time `json:"end_at" gorm:"index"`
	Groups      []Group   `json:"-" gorm:"many2many:contest_groups;"`
	// Type        string    `gorm:"size:32"` // NOI, IOI etc.
	Problems      []Problem `json:"-" gorm:"many2many:contest_problems;"`
	CreatedBy     string    `json:"created_by" gorm:"size:128"`
	CreatedAt     time.Time `json:"created_at"`
	RatingSettled bool      `json:"rating_settled" gorm:"default:false"` // Marked true when rating settlement is complete (only once, even on failure)
}

// ContestRatingHistory records rating changes after a contest ends.
type ContestRatingHistory struct {
	ID           uint   `gorm:"primaryKey"`
	Username     string `gorm:"index;size:128"`
	ContestID    uint   `gorm:"index"`
	ContestName  string `gorm:"size:256"`
	Rank         int    `json:"rank"`          // Final rank in contest (considering ties)
	TotalScore   int    `json:"total_score"`   // Total score achieved
	RatingBefore int    `json:"rating_before"` // Rating before contest
	RatingAfter  int    `json:"rating_after"`  // Rating after contest
	RatingChange int    `json:"rating_change"` // Rating delta (after - before)
	CreatedAt    time.Time
}
