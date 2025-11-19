package web

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/minicago/gooj/sql_service"
)

// CodeFileHandler returns last submitted code and result for a user/problem
func CodeFileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	user := vars["user"]
	problem := vars["problem"]
	// fetch last submission from DB
	sub, results, err := sql_service.GetLastSubmission(user, problem)
	if err != nil {
		http.Error(w, "no submission", http.StatusNotFound)
		return
	}
	// return code and a summary
	summary := map[string]interface{}{"status": sub.Status, "test_results": results}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": sub.Code, "summary": summary})
}
