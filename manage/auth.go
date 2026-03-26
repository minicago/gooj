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

var tokenStore = make(map[string]tokenInfo)

func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func GenerateToken(username string) (string, time.Time) {
	token := generateToken()
	tokenStore[token] = tokenInfo{Username: username, Expiry: time.Now().Add(5 * time.Minute)}
	return token, tokenStore[token].Expiry
}

func ValidateToken(token string) bool {
	info, exists := tokenStore[token]
	if !exists || time.Now().After(info.Expiry) {
		return false
	}
	// Refresh token expiration
	info.Expiry = time.Now().Add(5 * time.Minute)
	tokenStore[token] = info
	return true
}

// GetUsernameFromToken returns the username bound to a token and whether it exists
func GetUsernameFromToken(token string) (string, bool) {
	info, exists := tokenStore[token]
	if !exists || time.Now().After(info.Expiry) {
		return "", false
	}
	return info.Username, true
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
