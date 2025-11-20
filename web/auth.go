package web

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/minicago/gooj/sql_service"
)

type authReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

var tokenStore = make(map[string]time.Time)

func generateToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	var req authReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" || req.Password == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	if err := sql_service.CreateUser(req.Username, req.Password); err != nil {
		http.Error(w, "create user failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	var req authReq
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Username == "" || req.Password == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	ok, err := sql_service.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate token and store with expiration
	token := generateToken()
	tokenStore[token] = time.Now().Add(5 * time.Minute)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "token": token})
}

func ValidateToken(token string) bool {
	expiry, exists := tokenStore[token]
	if !exists || time.Now().After(expiry) {
		return false
	}
	// Refresh token expiration
	tokenStore[token] = time.Now().Add(5 * time.Minute)
	return true
}
