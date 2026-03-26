package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/minicago/gooj/sql_service"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// ProblemsHandler returns paginated problems. Query params: page (1-based), per (default 10)
func ProblemsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := 1
	per := 10
	if p := q.Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if p := q.Get("per"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			per = v
		}
	}
	probs, total, err := sql_service.ListProblems(page, per)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := map[string]interface{}{"problems": probs, "total": total, "page": page, "per": per}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// LastSubmissionHandler returns last submission and results for username & problem query params
func LastSubmissionHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	user := q.Get("username")
	prob := q.Get("problem")
	if user == "" || prob == "" {
		http.Error(w, "missing params", http.StatusBadRequest)
		return
	}

	// Resolve problem ID or Name to actual problem name for consistency
	problemName := prob
	db := sql_service.DB()
	if db != nil {
		// Try to find problem by ID
		var problem sql_service.Problem
		if err := db.First(&problem, prob).Error; err == nil {
			// Found by ID, use the Name
			problemName = problem.Name
		} else {
			// Try to find by Name
			if err := db.Where("name = ?", prob).First(&problem).Error; err == nil {
				problemName = problem.Name
			}
		}
	}

	sub, results, err := sql_service.GetLastSubmission(user, problemName)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// if no submission, return empty
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"submission": sub, "results": results})
}

// ProblemDataHandler returns the statement.md and config.json for a given problem id or name
func ProblemDataHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	// Try to parse as ID first, if fails search by name
	var problem sql_service.Problem
	db := sql_service.DB()
	if db == nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// First try to find by ID
	err := db.First(&problem, id).Error
	if err != nil {
		// If not found by ID, try to find by Name
		err = db.Where("name = ?", id).First(&problem).Error
		if err != nil {
			http.Error(w, "problem not found", http.StatusNotFound)
			return
		}
	}

	// Use the problem ID as directory name
	problemID := strconv.FormatUint(uint64(problem.ID), 10)
	base := filepath.Join("data", "problem", problemID)
	stmtPath := filepath.Join(base, "statement.md")
	cfgPath := filepath.Join(base, "config.json")
	stmtBytes, err := os.ReadFile(stmtPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "statement not found", http.StatusNotFound)
			return
		}
		http.Error(w, "internal read error", http.StatusInternalServerError)
		return
	}
	cfgBytes, err := os.ReadFile(cfgPath)
	var cfg interface{}
	if err == nil {
		_ = json.Unmarshal(cfgBytes, &cfg)
	} else {
		// if config missing, leave cfg nil
		cfg = nil
	}
	// render markdown to HTML
	var buf bytes.Buffer
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.Typographer,
		),
	)
	_ = md.Convert(stmtBytes, &buf)
	out := map[string]interface{}{"statement": string(stmtBytes), "statement_html": buf.String(), "config": cfg}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
