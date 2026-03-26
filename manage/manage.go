package manage

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
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
	// Get creator from request context
	creator := CurrentUsername(r)
	g := sql_service.Group{Name: req.GroupName, CreatedBy: creator}
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

	// 检查用户组权限不能超过自身权限
	if !CheckUserPermission(creator, "GroupPermission") {
		if g.EditPermission && !CheckUserPermission(creator, "EditPermission") {
			http.Error(w, "permission denied: cannot grant EditPermission", http.StatusForbidden)
			return
		}
		if g.UserPermission && !CheckUserPermission(creator, "UserPermission") {
			http.Error(w, "permission denied: cannot grant UserPermission", http.StatusForbidden)
			return
		}
		if g.GroupPermission && !CheckUserPermission(creator, "GroupPermission") {
			http.Error(w, "permission denied: cannot grant GroupPermission", http.StatusForbidden)
			return
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

// ListGroupsHandler returns all groups and their details (public access for registration)
func ListGroupsHandler(w http.ResponseWriter, r *http.Request) {
	if db == nil {
		http.Error(w, "database not initialized", http.StatusInternalServerError)
		return
	}
	var groups []sql_service.Group
	if err := db.Find(&groups).Error; err != nil {
		http.Error(w, "failed to fetch groups", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"groups": groups,
		"total":  len(groups),
	})
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

// DeleteProblemHandler deletes a problem by ID (admin only)
func DeleteProblemHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		ProblemID uint `json:"problem_id"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.ProblemID == 0 {
		http.Error(w, "missing problem_id", http.StatusBadRequest)
		return
	}

	// Check if user has edit permission
	currentUser := CurrentUsername(r)
	if !CheckUserPermission(currentUser, "EditPermission") {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	// Delete problem from database
	if db == nil {
		http.Error(w, "database not initialized", http.StatusInternalServerError)
		return
	}

	// First, get problem directory before deleting from database
	problemDir := filepath.Join("data", "problem", fmt.Sprintf("%d", req.ProblemID))

	// Delete problem from database
	if err := db.Delete(&sql_service.Problem{ID: req.ProblemID}).Error; err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Also delete problem directory
	if err := os.RemoveAll(problemDir); err != nil {
		log.Printf("Warning: failed to delete problem directory %s: %v", problemDir, err)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// UpdateGroupCreatorHandler allows group creators to change the group's creator
func UpdateGroupCreatorHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		GroupName  string `json:"groupName"`
		NewCreator string `json:"newCreator"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.GroupName == "" || req.NewCreator == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	// Get current user
	currentUser := CurrentUsername(r)
	if currentUser == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Find the group
	var group sql_service.Group
	if err := db.Where("name = ?", req.GroupName).First(&group).Error; err != nil {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}

	// Check if current user is the creator of this group or has GroupPermission
	if group.CreatedBy != currentUser && !CheckUserPermission(currentUser, "GroupPermission") {
		http.Error(w, "only the group creator or users with GroupPermission can update the creator", http.StatusForbidden)
		return
	}

	// Check if new creator exists and is a valid user
	var newCreatorUser sql_service.User
	if err := db.Where("username = ?", req.NewCreator).First(&newCreatorUser).Error; err != nil {
		http.Error(w, "new creator user not found", http.StatusBadRequest)
		return
	}

	// Update group creator
	group.CreatedBy = req.NewCreator
	if err := db.Save(&group).Error; err != nil {
		http.Error(w, "failed to update group creator", http.StatusInternalServerError)
		return
	}

	// Update all users in this group to have the new creator
	if err := db.Model(&sql_service.User{}).Where("group_name = ?", req.GroupName).Update("created_by", req.NewCreator).Error; err != nil {
		http.Error(w, "failed to update users' creator", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// DeleteGroupHandler allows group creators to delete a group
func DeleteGroupHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		GroupName string `json:"groupName"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.GroupName == "" {
		http.Error(w, "missing group name", http.StatusBadRequest)
		return
	}

	// Get current user
	currentUser := CurrentUsername(r)
	if currentUser == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Find the group
	var group sql_service.Group
	if err := db.Where("name = ?", req.GroupName).First(&group).Error; err != nil {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}

	// Prevent deletion of super group
	if group.Name == "super" {
		http.Error(w, "cannot delete super group", http.StatusForbidden)
		return
	}

	// Check if current user is the creator of this group or has GroupPermission
	if group.CreatedBy != currentUser && !CheckUserPermission(currentUser, "GroupPermission") {
		http.Error(w, "only the group creator or users with GroupPermission can delete the group", http.StatusForbidden)
		return
	}

	// Check if there are users in this group
	var userCount int64
	if err := db.Model(&sql_service.User{}).Where("group_name = ?", req.GroupName).Count(&userCount).Error; err != nil {
		http.Error(w, "failed to check group users", http.StatusInternalServerError)
		return
	}

	if userCount > 0 {
		http.Error(w, "cannot delete group with existing users", http.StatusForbidden)
		return
	}

	// Delete the group
	if err := db.Delete(&group).Error; err != nil {
		http.Error(w, "failed to delete group", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
