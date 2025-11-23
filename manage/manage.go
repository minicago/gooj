package manage

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
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
	// permissions are optional for now; require username and group
	if req.Username == "" || req.Group == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	password := generateStrongPassword()
	// get creator from request context (set by auth middleware)
	creator := CurrentUsername(r)

	if err := sql_service.CreateUserWithGroup(req.Username, password, req.Group, creator); err != nil {
		http.Error(w, "create user failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "password": password})
}

// CreateGroupHandler creates a new user group with specified permissions
func CreateGroupHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		GroupName   string   `json:"groupName"`
		Permissions []string `json:"permissions"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.GroupName == "" || len(req.Permissions) == 0 {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	g := sql_service.Group{Name: req.GroupName}
	for _, p := range req.Permissions {
		switch p {
		case "EditPermission":
			g.EditPermission = true
		case "UserPermission":
			g.UserPermission = true
		case "GroupPermission":
			g.GroupPermission = true
		}
	}
	if err := db.Create(&g).Error; err != nil {
		http.Error(w, "create group failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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

	currentUser := CurrentUsername(r)

	if !CheckUserPermission(currentUser, "GroupPermission") {
		if err := db.Preload("Group").Where(&sql_service.User{CreatedBy: currentUser}).Find(&users).Error; err != nil {
			http.Error(w, "failed to fetch users", http.StatusInternalServerError)
			return
		}
	} else {
		if err := db.Preload("Group").Find(&users).Error; err != nil {
			http.Error(w, "failed to fetch users", http.StatusInternalServerError)
			return
		}
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
	if CheckUserPermission(CurrentUsername(r), "UserPermission") == false {
		http.Error(w, "permission denied", http.StatusForbidden)
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

	if username == "" || permission == "" {
		http.Error(w, "missing username or permission", http.StatusBadRequest)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"permited": CheckUserPermission(username, permission),
	})

}

func ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		Username string `json:"username"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" {
		http.Error(w, "missing username", http.StatusBadRequest)
		return
	}
	password := generateStrongPassword()
	if err := sql_service.ResetCreatedUserPassword(CurrentUsername(r), req.Username, password); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "password": password})
}

func DeleteUserHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		Username string `json:"username"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" {
		http.Error(w, "missing username", http.StatusBadRequest)
		return
	}

	if err := sql_service.DeleteCreatedUser(CurrentUsername(r), req.Username); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
