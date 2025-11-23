package web

import (
	"encoding/json"
	"net/http"

	"github.com/minicago/gooj/manage"
	"github.com/minicago/gooj/sql_service"
)

type authReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
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
