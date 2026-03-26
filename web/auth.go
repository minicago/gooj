package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/minicago/gooj/manage"
	"github.com/minicago/gooj/sql_service"
	"golang.org/x/crypto/bcrypt"
)

type authReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Group    string `json:"group"` // Group name
}

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req registerReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	// 验证输入
	if req.Username == "" || req.Password == "" || req.Group == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	// 检查用户名是否已存在
	var existingUser sql_service.User
	if err := sql_service.DB().Where("username = ?", req.Username).First(&existingUser).Error; err == nil {
		http.Error(w, "username already exists", http.StatusBadRequest)
		return
	}

	// 检查组是否存在
	var group sql_service.Group
	if err := sql_service.DB().Where("name = ?", req.Group).First(&group).Error; err != nil {
		http.Error(w, "group not found", http.StatusBadRequest)
		return
	}

	// 哈希密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	// 创建新用户（未审核状态），CreatedBy 设为组的创建者，角色默认为 "user"
	newUser := sql_service.User{
		Username:  req.Username,
		Password:  string(hashedPassword),
		GroupName: req.Group,
		CreatedBy: group.CreatedBy, // 使用组的创建者
		Approved:  false,
	}

	if err := sql_service.DB().Create(&newUser).Error; err != nil {
		http.Error(w, "create user failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Registration submitted for approval",
	})
}

// ApproveUserHandler allows group creators to approve user registrations
func ApproveUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]
	if username == "" {
		http.Error(w, "missing username", http.StatusBadRequest)
		return
	}

	// 获取当前登录用户
	currentUser, err := getCurrentUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// 查找待审核用户
	var user sql_service.User
	if err := sql_service.DB().Where("username = ? AND approved = ?", username, false).First(&user).Error; err != nil {
		http.Error(w, "user not found or already approved", http.StatusNotFound)
		return
	}

	// 检查组是否存在且创建者匹配
	var group sql_service.Group
	if err := sql_service.DB().Where("name = ?", user.GroupName).First(&group).Error; err != nil {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}

	// 检查权限：要么是组的创建者，要么有 GroupPermission
	if group.CreatedBy != currentUser.Username {
		if !currentUser.Group.GroupPermission {
			http.Error(w, "not authorized to approve this user, group created by another user : "+group.CreatedBy+", you are "+currentUser.Username, http.StatusForbidden)
			return
		}
	}

	// 批准用户
	now := time.Now()
	user.Approved = true
	user.ApprovedAt = &now
	user.ApprovedBy = currentUser.Username

	if err := sql_service.DB().Save(&user).Error; err != nil {
		http.Error(w, "failed to approve user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// GetUsersHandler returns list of users with create permission (for creator selection)
func GetUsersHandler(w http.ResponseWriter, r *http.Request) {
	var users []sql_service.User

	if err := sql_service.DB().Preload("Group").Find(&users).Error; err != nil {
		http.Error(w, "failed to get users", http.StatusInternalServerError)
		return
	}

	// 只返回可修改的用户
	var subusers []sql_service.User
	for _, user := range users {
		// fmt.Print(user.ID, " ", user.GroupName, " ", user.Group.CreatedBy, " ", manage.CurrentUsername(r), " ", user.Group.GroupPermission, "\n")
		if user.Group.CreatedBy == manage.CurrentUsername(r) || manage.CheckUserPermission(manage.CurrentUsername(r), "GroupPermission") {
			subusers = append(subusers, user)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"users": subusers,
		"total": len(subusers),
	})
}

// GetAllUsersHandler returns all users (for registration creator selection)
func GetAllUsersHandler(w http.ResponseWriter, r *http.Request) {
	var users []sql_service.User
	if err := sql_service.DB().Preload("Group").Find(&users).Error; err != nil {
		http.Error(w, "failed to get all users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"users": users,
		"total": len(users),
	})
}

// GetPendingUsersHandler returns list of users waiting for approval
func GetPendingUsersHandler(w http.ResponseWriter, r *http.Request) {
	var users []sql_service.User
	if err := sql_service.DB().Preload("Group").Where("approved = ?", false).Find(&users).Error; err != nil {
		http.Error(w, "failed to get pending users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"users": users,
		"total": len(users),
	})
}

// GetApprovedUsersHandler returns list of approved users
func GetApprovedUsersHandler(w http.ResponseWriter, r *http.Request) {
	var users []sql_service.User
	if err := sql_service.DB().Preload("Group").Where("approved = ?", true).Find(&users).Error; err != nil {
		http.Error(w, "failed to get approved users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"users": users,
		"total": len(users),
	})
}

// RejectUserHandler allows group creators to reject user registrations
func RejectUserHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	username := vars["username"]
	if username == "" {
		http.Error(w, "missing username", http.StatusBadRequest)
		return
	}

	// 获取当前登录用户
	currentUser, err := getCurrentUser(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// 查找待审核用户
	var user sql_service.User
	if err := sql_service.DB().Where("username = ? AND approved = ?", username, false).First(&user).Error; err != nil {
		http.Error(w, "user not found or already processed", http.StatusNotFound)
		return
	}

	// 检查组是否存在且创建者匹配
	var group sql_service.Group
	if err := sql_service.DB().Where("name = ?", user.GroupName).First(&group).Error; err != nil {
		http.Error(w, "group not found", http.StatusNotFound)
		return
	}

	// 检查权限：要么是组的创建者，要么有 GroupPermission
	if group.CreatedBy != currentUser.Username {
		if !currentUser.Group.GroupPermission {
			http.Error(w, "not authorized to reject this user", http.StatusForbidden)
			return
		}
	}

	// 删除用户申请
	if err := sql_service.DB().Delete(&user).Error; err != nil {
		http.Error(w, "failed to reject user", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// LoginHandler now checks if user is approved
func LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req authReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" || req.Password == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	// 使用 AuthenticateUser 验证密码（支持 bcrypt 哈希比较）
	ok, err := sql_service.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "用户名或密码错误", http.StatusUnauthorized)
		return
	}

	// 查找用户并检查审核状态
	var user sql_service.User
	if err := sql_service.DB().Where("username = ?", req.Username).First(&user).Error; err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// 检查用户是否已通过审核
	if !user.Approved {
		http.Error(w, "account not approved by creator", http.StatusForbidden)
		return
	}

	// Generate token and store with expiration and username
	token, time := manage.GenerateToken(req.Username)

	// set cookie so that browser requests automatically include token
	http.SetCookie(w, &http.Cookie{
		Name:    "auth_token",
		Value:   token,
		Path:    "/",
		Expires: time,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "token": token})
}

// Helper function to get current user from request
func getCurrentUser(r *http.Request) (*sql_service.User, error) {
	// 从token获取当前用户信息
	var token string
	if c, err := r.Cookie("auth_token"); err == nil {
		token = c.Value
	} else if auth := r.Header.Get("Authorization"); auth != "" && strings.HasPrefix(auth, "Bearer ") {
		token = strings.TrimPrefix(auth, "Bearer ")
	} else if h := r.Header.Get("X-Auth-Token"); h != "" {
		token = h
	}

	if token == "" {
		return nil, fmt.Errorf("no token provided")
	}

	username, ok := manage.GetUsernameFromToken(token)
	if !ok {
		return nil, fmt.Errorf("invalid token")
	}

	var user sql_service.User
	if err := sql_service.DB().Preload("Group").Where("username = ?", username).First(&user).Error; err != nil {
		return nil, fmt.Errorf("user not found")
	}

	return &user, nil
}
