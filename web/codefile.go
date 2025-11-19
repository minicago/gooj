package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/minicago/gooj/file_service"
)

// CodeFileHandler returns last submitted code and result for a user/problem
func CodeFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	user := vars["user"]
	problem := vars["problem"]
	svc := file_service.Default()
	if svc == nil {
		http.Error(w, "server not ready", http.StatusInternalServerError)
		return
	}
	codePath := filepath.Join("data", "user", user, problem+".cpp")
	resultPath := filepath.Join("data", "user", user, problem+".result")
	code, _ := svc.ReadFile(codePath)
	res, _ := svc.ReadFile(resultPath)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"code": string(code), "result": string(res)})
}
