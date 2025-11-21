package sql_service

import "time"

// User model represents a user in the system
type User struct {
	ID        uint   `gorm:"primaryKey"`
	Username  string `gorm:"uniqueIndex;size:128"`
	Password  string
	Group     Group `gorm:"foreignKey:Name;references:GroupName;"` // User group
	GroupName string
	CreatedAt time.Time
	CreatedBy string `gorm:"size:128"` // Username of the creator
}

type Group struct {
	ID              uint   `gorm:"primaryKey"`
	Name            string `gorm:"uniqueIndex;size:128"`
	EditPermission  bool
	UserPermission  bool
	GroupPermission bool
}

// Permission model represents a structured permission type
// type Permission struct {
// 	ID   uint   `gorm:"primaryKey"`
// 	Name string `gorm:"uniqueIndex;size:128"` // Permission name, e.g., edit_problems
// }

// Submission model represents a code submission
type Submission struct {
	ID          uint   `gorm:"primaryKey"`
	Username    string `gorm:"index;size:128"`
	Problem     string `gorm:"size:128"`
	Code        string `gorm:"type:text"`
	Status      string `gorm:"size:32"` // queued, running, ok, wa, compile_error, runtime_error
	CreatedAt   time.Time
	UpdatedAt   time.Time
	TestResults []TestResult `gorm:"foreignKey:SubmissionID"`
}

// TestResult model represents the result of a test case
type TestResult struct {
	ID           uint `gorm:"primaryKey"`
	SubmissionID uint `gorm:"index"`
	TestIndex    int  `gorm:"column:test_index"`
	Passed       bool
	Output       string `gorm:"type:text"`
	Expected     string `gorm:"type:text"`
	TimeMs       int
	MemoryKB     int
}

// Problem model represents a coding problem
type Problem struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"uniqueIndex;size:128"`
	Title       string `gorm:"size:256"`
	Description string `gorm:"type:text"`
	TestsCount  int
	TimeLimitMs int
	MemLimitMB  int
}
