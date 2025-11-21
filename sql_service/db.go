package sql_service

import (
	"os"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

// Group represents a user group with specific permissions
// type Group struct {
// 	Name      string `gorm:"primaryKey"` // Group name as the primary key
// 	CanEdit   bool   // Permission to edit problems
// 	CanSubmit bool   // Permission to submit solutions
// 	CanView   bool   // Permission to view problems
// }

// Init initializes the SQLite database at the specified path
func Init(path string) error {
	var err error
	db, err = gorm.Open(sqlite.Open(path), &gorm.Config{})
	db.Logger = logger.Default.LogMode(logger.Silent)
	if err != nil {
		return err
	}
	// Migrate the schema
	if err := db.AutoMigrate(&User{}, &Group{}, &Submission{}, &TestResult{}, &Problem{}); err != nil {
		return err
	}
	// Load problems from file if it exists
	if _, err := os.Stat("data/problem_list.json"); err == nil {
		_ = loadProblemsFromFile("data/problem_list.json")
	}

	return nil
}

// DB returns the database instance
func DB() *gorm.DB {
	return db
}
