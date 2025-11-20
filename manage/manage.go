package manage

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"time"

	"github.com/minicago/gooj/sql_service"
)

func init() {
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
	if err := sql_service.CreateUserWithGroup(req.Username, password, req.Group, req.Permissions); err != nil {
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
