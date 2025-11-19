package sql_service

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

// Models
type User struct {
	ID        uint   `gorm:"primaryKey"`
	Username  string `gorm:"uniqueIndex;size:128"`
	Password  string
	CreatedAt time.Time
}

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

type Problem struct {
	ID          uint   `gorm:"primaryKey"`
	Name        string `gorm:"uniqueIndex;size:128"`
	Title       string `gorm:"size:256"`
	Description string `gorm:"type:text"`
	TestsCount  int
	TimeLimitMs int
	MemLimitMB  int
}

// Init initializes the sqlite database at path (create file if not exists)
func Init(path string) error {
	var err error
	db, err = gorm.Open(sqlite.Open(path), &gorm.Config{})
	db.Logger = logger.Default.LogMode(logger.Silent)
	if err != nil {
		return err
	}
	// migrate
	if err := db.AutoMigrate(&User{}, &Submission{}, &TestResult{}, &Problem{}); err != nil {
		return err
	}
	// try to load problems from data/problem_list.json if exists
	if _, err := os.Stat("data/problem_list.json"); err == nil {
		_ = loadProblemsFromFile("data/problem_list.json")
	}
	return nil
}

func DB() *gorm.DB { return db }

func CreateUserIfNotExists(username string) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	var u User
	if err := db.Where("username = ?", username).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			u = User{Username: username}
			return db.Create(&u).Error
		}
		return err
	}
	return nil
}

// CreateUser registers a user with a plain password (hashed with bcrypt)
func CreateUser(username, password string) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u := User{Username: username, Password: string(hashed)}
	return db.Create(&u).Error
}

// AuthenticateUser verifies username/password
func AuthenticateUser(username, password string) (bool, error) {
	if db == nil {
		return false, errors.New("db not initialized")
	}
	var u User
	if err := db.Where("username = ?", username).First(&u).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)); err != nil {
		return false, nil
	}
	return true, nil
}

func CreateSubmission(username, problem, code string) (Submission, error) {
	if db == nil {
		return Submission{}, errors.New("db not initialized")
	}
	s := Submission{Username: username, Problem: problem, Code: code, Status: "queued"}
	if err := db.Create(&s).Error; err != nil {
		return Submission{}, err
	}
	return s, nil
}

// PopQueuedSubmission atomically finds the oldest queued submission and marks it running
func PopQueuedSubmission() (Submission, error) {
	if db == nil {
		return Submission{}, errors.New("db not initialized")
	}
	var s Submission
	tx := db.Begin()
	if tx.Error != nil {
		return Submission{}, tx.Error
	}
	if err := tx.Set("gorm:query_option", "FOR UPDATE").Where("status = ?", "queued").Order("created_at asc").First(&s).Error; err != nil {
		tx.Rollback()
		return Submission{}, err
	}
	if err := tx.Model(&s).Update("status", "running").Error; err != nil {
		tx.Rollback()
		return Submission{}, err
	}
	if err := tx.Commit().Error; err != nil {
		return Submission{}, err
	}
	// reload to get updated timestamps
	_ = db.First(&s, s.ID).Error
	return s, nil
}

func UpdateSubmissionResult(subID uint, status string, results []TestResult) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Submission{}).Where("id = ?", subID).Updates(map[string]interface{}{"status": status, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		for i := range results {
			results[i].SubmissionID = subID
			if err := tx.Create(&results[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func GetLastSubmission(username, problem string) (Submission, []TestResult, error) {
	if db == nil {
		return Submission{}, nil, errors.New("db not initialized")
	}
	var s Submission
	if err := db.Where("username = ? AND problem = ?", username, problem).Order("created_at desc").First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// silent not found: return zero values without error
			return Submission{}, nil, nil
		}
		return Submission{}, nil, err
	}
	var results []TestResult
	_ = db.Where("submission_id = ?", s.ID).Order("test_index asc").Find(&results).Error
	return s, results, nil
}

// loadProblemsFromFile reads problem_list.json and inserts/updates Problem records
func loadProblemsFromFile(path string) error {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	// expect array of either strings or objects
	var raw []map[string]interface{}
	if err := json.Unmarshal(b, &raw); err == nil {
		for _, item := range raw {
			name, _ := item["name"].(string)
			if name == "" {
				// maybe it's a single string in alternative format; skip
				continue
			}
			title, _ := item["title"].(string)
			desc, _ := item["description"].(string)
			testsCount := 0
			if v, ok := item["tests_count"].(float64); ok {
				testsCount = int(v)
			}
			timeLimit := 0
			if v, ok := item["time_limit_ms"].(float64); ok {
				timeLimit = int(v)
			}
			memLimit := 0
			if v, ok := item["mem_limit_mb"].(float64); ok {
				memLimit = int(v)
			}
			p := Problem{Name: name, Title: title, Description: desc, TestsCount: testsCount, TimeLimitMs: timeLimit, MemLimitMB: memLimit}
			// upsert
			_ = db.Where(Problem{Name: name}).Assign(p).FirstOrCreate(&p).Error
		}
		return nil
	}
	// try simple string array
	var arr []string
	if err := json.Unmarshal(b, &arr); err == nil {
		for _, name := range arr {
			p := Problem{Name: name, Title: name}
			_ = db.Where(Problem{Name: name}).FirstOrCreate(&p).Error
		}
		return nil
	}
	return nil
}

// ListProblems returns problems for pagination (page 1-based)
func ListProblems(page, perPage int) ([]Problem, int64, error) {
	if db == nil {
		return nil, 0, errors.New("db not initialized")
	}
	var total int64
	if err := db.Model(&Problem{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var probs []Problem
	offset := (page - 1) * perPage
	if err := db.Order("id asc").Offset(offset).Limit(perPage).Find(&probs).Error; err != nil {
		return nil, 0, err
	}
	return probs, total, nil
}
