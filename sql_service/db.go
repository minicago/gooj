package sql_service

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/minicago/gooj/config"
)

var db *gorm.DB

// Group represents a user group with specific permissions
// type Group struct {
// 	Name      string `gorm:"primaryKey"` // Group name as the primary key
// 	CanEdit   bool   // Permission to edit problems
// 	CanSubmit bool   // Permission to submit solutions
// 	CanView   bool   // Permission to view problems
// }

// Init initializes the database based on configuration
func Init() error {
	var err error
	dbType := config.GetDatabaseType()

	switch dbType {
	case config.DatabaseTypeSQLite:
		path := config.GetSQLitePath()
		db, err = gorm.Open(sqlite.Open(path), &gorm.Config{})
		if err != nil {
			return fmt.Errorf("failed to open SQLite database: %w", err)
		}
	case config.DatabaseTypeMySQL:
		mysqlCfg := config.GetMySQLConfig()
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
			mysqlCfg.User, mysqlCfg.Password, mysqlCfg.Host, mysqlCfg.Port, mysqlCfg.DBName)
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
		if err != nil {
			return fmt.Errorf("failed to open MySQL database: %w", err)
		}
	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}

	db.Logger = logger.Default.LogMode(logger.Silent)

	// Migrate the schema
	if err := db.AutoMigrate(&User{}, &Group{}, &Submission{}, &TestResult{}, &Problem{}, &Contest{}, &ContestRatingHistory{}, &SimilarityRecord{}); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// Load problems from file if it exists
	// if _, err := os.Stat("data/problem_list.json"); err == nil {
	// 	_ = loadProblemsFromFile("data/problem_list.json")
	// }

	return nil
}

// DB returns the database instance
func DB() *gorm.DB {
	return db
}
