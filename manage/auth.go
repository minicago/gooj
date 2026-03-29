package manage

import (
	"context"
	"encoding/base64"
	"math/rand"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/minicago/gooj/sql_service"
)

type tokenOpType int

var tokenOpChan chan tokenOp

func InitTokenStore() {
	tokenOpChan = make(chan tokenOp)
	go tokenServer()
}

func tokenServer() {
	store := make(map[string]tokenInfo)
	for op := range tokenOpChan {
		switch op.op {
		case tokenOpSet:
			store[op.token] = op.info
			if op.result != nil {
				op.result <- tokenOpResult{valid: true}
			}
		case tokenOpGet:
			info, exists := store[op.token]
			if op.result != nil {
				op.result <- tokenOpResult{info: info, exists: exists}
			}
		case tokenOpValidate:
			info, exists := store[op.token]
			valid := exists && !time.Now().After(info.Expiry)
			if valid {
				// refresh expiry
				info.Expiry = time.Now().Add(60 * time.Minute)
				store[op.token] = info
			}
			if op.result != nil {
				op.result <- tokenOpResult{info: info, exists: exists, valid: valid}
			}
		}
	}
}

const (
	tokenOpSet tokenOpType = iota
	tokenOpGet
	tokenOpValidate
)

type tokenOp struct {
	op     tokenOpType
	token  string
	info   tokenInfo
	result chan tokenOpResult
}

type tokenOpResult struct {
	info   tokenInfo
	exists bool
	valid  bool
}

type contextKey string

const usernameContextKey contextKey = "username"

// GetUserPermissionsHandler returns the permissions of a user based on their group
func CheckUserPermission(username string, permission string) bool {

	var user sql_service.User

	if err := db.Preload("Group").Where("username = ? ", username).First(&user).Error; err != nil {
		return false
	}

	// Check if Group is loaded
	if user.Group.Name == "" {
		return false
	}

	// Use reflection to get the permission field
	groupValue := reflect.ValueOf(user.Group)
	field := groupValue.FieldByName(permission)

	// Check if field exists and is a bool
	if !field.IsValid() || field.Kind() != reflect.Bool {
		return false
	}

	return field.Bool()

}

type tokenInfo struct {
	Username string
	Expiry   time.Time
}

func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func GenerateToken(username string) (string, time.Time) {
	token := generateToken()
	expiry := time.Now().Add(60 * time.Minute)
	result := make(chan tokenOpResult)
	tokenOpChan <- tokenOp{
		op:     tokenOpSet,
		token:  token,
		info:   tokenInfo{Username: username, Expiry: expiry},
		result: result,
	}
	<-result // wait for set
	return token, expiry
}

func ValidateToken(token string) bool {
	result := make(chan tokenOpResult)
	tokenOpChan <- tokenOp{
		op:     tokenOpValidate,
		token:  token,
		result: result,
	}
	res := <-result
	return res.valid
}

// GetUsernameFromToken returns the username bound to a token and whether it exists
func GetUsernameFromToken(token string) (string, bool) {
	result := make(chan tokenOpResult)
	tokenOpChan <- tokenOp{
		op:     tokenOpGet,
		token:  token,
		result: result,
	}
	res := <-result
	if !res.exists || time.Now().After(res.info.Expiry) {
		return "", false
	}
	return res.info.Username, true
}

func CurrentUsername(r *http.Request) string {
	username := r.Context().Value(usernameContextKey)
	if usernameStr, ok := username.(string); ok {
		return usernameStr
	}
	return ""
}

func AuthMiddleWare(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// public paths
		if strings.HasPrefix(path, "/static/") || path == "/" || path == "/login" || path == "/register" || path == "/api/allUsers" || path == "/api/groups" {
			next.ServeHTTP(w, r)
			return
		}

		// extract token from cookie or headers
		var token string
		if c, err := r.Cookie("auth_token"); err == nil {
			token = c.Value
		} else if auth := r.Header.Get("Authorization"); auth != "" && strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		} else if h := r.Header.Get("X-Auth-Token"); h != "" {
			token = h
		}

		if token == "" || !ValidateToken(token) {
			http.SetCookie(w, &http.Cookie{
				Name:   "auth_token",
				Value:  "",
				MaxAge: -1,
			})
			if r.Method == "GET" {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)

			return
		} else {
			// attach username to request context and check if user is approved
			if username, ok := GetUsernameFromToken(token); ok {
				// Check if user is approved
				var user sql_service.User
				if err := db.Where("username = ? AND approved = ?", username, true).First(&user).Error; err != nil {
					http.SetCookie(w, &http.Cookie{
						Name:   "auth_token",
						Value:  "",
						MaxAge: -1,
					})
					if r.Method == "GET" {
						http.Redirect(w, r, "/", http.StatusFound)
						return
					}
					http.Error(w, "account not approved", http.StatusForbidden)
					return
				}
				ctx := context.WithValue(r.Context(), usernameContextKey, username)
				r = r.WithContext(ctx)
			}
		}
		next.ServeHTTP(w, r)
	})
}
