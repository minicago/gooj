package web

import (
	"encoding/json"
	"net/http"

	"github.com/minicago/gooj/config"
	"github.com/minicago/gooj/sql_service"
)

// SetRatingConfigHandler POST /api/rating/config
// Updates the rating calculation weights (K-factor and rank/score blend weight)
// at runtime and persists them to the config file.
func SetRatingConfigHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireEdit(w, r); !ok {
		return
	}
	var req struct {
		KFactor    int     `json:"k_factor"`
		RankWeight float64 `json:"rank_weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if err := config.SetRatingConfig(req.KFactor, req.RankWeight); err != nil {
		http.Error(w, "failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "ok",
		"k_factor":    config.GetRatingKFactor(),
		"rank_weight": config.GetRatingRankWeight(),
	})
}

// GetRatingConfigHandler GET /api/rating/config
// Returns the currently configured rating weights.
func GetRatingConfigHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireEdit(w, r); !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"k_factor":    config.GetRatingKFactor(),
		"rank_weight": config.GetRatingRankWeight(),
	})
}

// RecomputeRatingsHandler POST /api/rating/recompute
// Rebuilds every user's rating from scratch using the current weights.
func RecomputeRatingsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireEdit(w, r); !ok {
		return
	}
	if err := sql_service.RecomputeAllRatings(); err != nil {
		http.Error(w, "failed to recompute ratings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONMessage(w, "ok", "all ratings recomputed")
}

// RecalculateContestRatingHandler POST /api/rating/recompute/{id}
// Recomputes rating for a specific contest (rebuilds the whole chain so weights
// are applied consistently).
func RecalculateContestRatingHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireEdit(w, r); !ok {
		return
	}
	id, err := parseIDParam(r)
	if err != nil {
		http.Error(w, "invalid contest id", http.StatusBadRequest)
		return
	}
	if err := sql_service.RecalculateContestRating(uint(id)); err != nil {
		http.Error(w, "failed to recompute contest rating: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONMessage(w, "ok", "contest rating recomputed")
}
