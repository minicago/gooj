package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/minicago/gooj/similarity"
	"github.com/minicago/gooj/sql_service"
)

// SimilarityCheckHandler POST /api/similarity/check?problem=<id>&threshold=<0..1>
// Triggers a pairwise code-similarity check for a problem and stores the results.
func SimilarityCheckHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireEdit(w, r); !ok {
		return
	}
	problemID, err := parseProblemID(r.URL.Query().Get("problem"))
	if err != nil {
		http.Error(w, "invalid problem id", http.StatusBadRequest)
		return
	}
	threshold := similarity.DefaultThreshold
	if tStr := r.URL.Query().Get("threshold"); tStr != "" {
		if t, e := strconv.ParseFloat(tStr, 64); e == nil && t > 0 && t <= 1 {
			threshold = t
		}
	}
	records, err := similarity.CheckProblem(problemID, threshold)
	if err != nil {
		http.Error(w, "similarity check failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "ok",
		"problem_id": problemID,
		"count":      len(records),
		"records":    records,
	})
}

// SimilarityListHandler GET /api/similarity?problem=<id>
// Lists stored similarity records for a problem.
func SimilarityListHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireEdit(w, r); !ok {
		return
	}
	problemID, err := parseProblemID(r.URL.Query().Get("problem"))
	if err != nil {
		http.Error(w, "invalid problem id", http.StatusBadRequest)
		return
	}
	records, err := sql_service.GetSimilarityByProblem(problemID)
	if err != nil {
		http.Error(w, "failed to load similarity: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"problem_id": problemID,
		"records":    records,
	})
}
