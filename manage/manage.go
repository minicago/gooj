package manage

import (
	"encoding/json"
	"errors"
	"log"
	"math/rand"
	"net/http"
	"reflect"
	"time"

	"gorm.io/gorm"

	"github.com/minicago/gooj/sql_service"
)

var db *gorm.DB

func Init() {
	db = sql_service.DB()
	if err := sql_service.EnsureSuperGroupAndRoot(); err != nil {
		log.Fatalf("super root fail: %v", err.Error())
	}
	rand.Seed(time.Now().UnixNano())
}

func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		Username    string `json:"username"`
		Group       string `json:"group"`
		Permissions string `json:"permissions"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" || req.Group == "" || req.Permissions == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	password := generateStrongPassword()
	if err := sql_service.CreateUserWithGroup(req.Username, password, req.Group); err != nil {
		http.Error(w, "create user failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "password": password})
}

func generateStrongPassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 8
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// ListUsersHandler returns all users and their details
func ListUsersHandler(w http.ResponseWriter, r *http.Request) {

	if db == nil {
		http.Error(w, "database not initialized", http.StatusInternalServerError)
		return
	}
	var users []sql_service.User
	if err := db.Preload("Group").Find(&users).Error; err != nil {
		http.Error(w, "failed to fetch users", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(users)
}

// ListGroupsHandler returns all groups and their details
func ListGroupsHandler(w http.ResponseWriter, r *http.Request) {

	if db == nil {
		http.Error(w, "database not initialized", http.StatusInternalServerError)
		return
	}
	var groups []sql_service.Group
	if err := db.Find(&groups).Error; err != nil {
		http.Error(w, "failed to fetch users", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(groups)
}

// GetUserPermissionsHandler returns the permissions of a user based on their group
func GetUserPermissionsHandler(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	permission := r.URL.Query().Get("permission")

	if username == "" {
		http.Error(w, "missing username", http.StatusBadRequest)
		return
	}
	var user sql_service.User

	if err := db.Preload("Group").Where("username = ? ", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "not permitted", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to fetch user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"permited": reflect.ValueOf(user.Group).FieldByName(permission).Bool(),
	})
}
