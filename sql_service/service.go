package sql_service

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// CreateUserIfNotExists registers a user with a plain password (hashed with bcrypt)
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

// CreateUserWithGroup creates a user with a specific group
func CreateUserWithGroup(username, password, group, createdBy string) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	// var groupData Group
	// db.Where(&Group{Name: group}).First(&groupData)
	u := User{Username: username, Password: string(hashed), GroupName: group, CreatedBy: createdBy}
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

// func QueryCreatedUserPassword(currentUsername, targetUsername string) (string, error) {
// 	if db == nil {
// 		return "", errors.New("db not initialized")
// 	}
// 	var currentUser, targetUser User
// 	if err := db.Where("username = ?", currentUsername).First(&currentUser).Error; err != nil {
// 		return "", err
// 	}
// 	if err := db.Where("username = ?", targetUsername).First(&targetUser).Error; err != nil {
// 		return "", err
// 	}
// 	if !currentUser.Group.UserPermission || targetUser.CreatedBy != currentUser.Username && !currentUser.Group.GroupPermission {
// 		return "", errors.New("permission denied")
// 	}
// 	return targetUser.Password, nil
// }

func ResetCreatedUserPassword(currentUsername, targetUsername, newPassword string) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	var currentUser, targetUser User
	if err := db.Preload("Group").Where("username = ?", currentUsername).First(&currentUser).Error; err != nil {
		return err
	}
	if err := db.Where("username = ?", targetUsername).First(&targetUser).Error; err != nil {
		return err
	}

	if !currentUser.Group.UserPermission || targetUser.CreatedBy != currentUser.Username && !currentUser.Group.GroupPermission {
		return errors.New("permission denied")
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	targetUser.Password = string(hashedPassword)

	if err := db.Save(&targetUser).Error; err != nil {
		return err
	}

	return nil
}

func DeleteCreatedUser(currentUsername, targetUsername string) error {
	if db == nil {
		return errors.New("db not initialized")
	}
	var currentUser, targetUser User
	if err := db.Preload("Group").Where("username = ?", currentUsername).First(&currentUser).Error; err != nil {
		return err
	}
	if err := db.Where("username = ?", targetUsername).First(&targetUser).Error; err != nil {
		return err
	}

	if !currentUser.Group.UserPermission || targetUser.CreatedBy != currentUser.Username && !currentUser.Group.GroupPermission {
		return errors.New("permission denied")
	}
	return db.Delete(&targetUser).Error
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
	b, err := os.ReadFile(path)
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

func EnsureSuperGroupAndRoot() error {
	if db == nil {
		return errors.New("db not initialized")
	}
	// Ensure super user group exists

	var superGroup Group
	if err := db.Where(&Group{Name: "super"}).First(&superGroup).Error; err != nil {

		if errors.Is(err, gorm.ErrRecordNotFound) {

			superGroup = Group{Name: "super", EditPermission: true, UserPermission: true, GroupPermission: true}
			if err := db.Save(&superGroup).Error; err != nil {
				return err
			}
		} else {

			return err
		}

	} else {
		superGroup.EditPermission = true
		superGroup.GroupPermission = true
		superGroup.UserPermission = true
		if err := db.Save(&superGroup).Error; err != nil {
			return err
		}
	}

	// Ensure root user exists
	var root User
	password := generateStrongPassword()
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	if err := db.Where("username = ?", "root").First(&root).Error; err != nil {

		if errors.Is(err, gorm.ErrRecordNotFound) {
			root = User{Username: "root", Password: string(hashedPassword), CreatedBy: "root", GroupName: "super"}
			if err := db.Create(&root).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	}

	root.GroupName = "super"

	// Update root password in case it already exists
	root.Password = string(hashedPassword)

	if err := db.Save(&root).Error; err != nil {
		print(err.Error())
		return err
	}

	// Save root password to file
	if err := os.WriteFile("data/rootpassword.txt", []byte(password), 0644); err != nil {
		return err
	}

	return nil
}

func generateStrongPassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 8
	seed := time.Now().UnixNano()
	randGen := rand.New(rand.NewSource(seed))
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[randGen.Intn(len(charset))]
	}
	return string(b)
}

func ResetUserPassword(username string, newPassword string) error {
	if db == nil {
		return errors.New("db not initialized")
	}

	var user User
	if err := db.Where("username = ?", username).First(&user).Error; err != nil {
		return err
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.Password = string(hashedPassword)

	if err := db.Save(&user).Error; err != nil {
		return err
	}

	return nil
}
