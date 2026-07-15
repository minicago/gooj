package web

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/minicago/gooj/manage"
	"github.com/minicago/gooj/sql_service"
)

// UserProfile represents user profile data for the homepage
type UserProfile struct {
	Username    string                `json:"username"`
	GroupName   string                `json:"group_name"`
	Rating      int                   `json:"rating"`
	Role        string                `json:"role"`
	SolvedCount int                   `json:"solved_count"`
	SolvedIDs   []uint                `json:"solved_ids"`
	TotalSub    int                   `json:"total_submissions"`
	ACSub       int                   `json:"accepted_submissions"`
	CreatedAt   string                `json:"created_at"`
	Contests    []ContestHistoryEntry `json:"contests"`
}

// ContestHistoryEntry represents a user's contest participation record
type ContestHistoryEntry struct {
	ContestName  string `json:"contest_name"`
	ContestID    uint   `json:"contest_id"`
	Rank         int    `json:"rank"`
	TotalScore   int    `json:"total_score"`
	RatingBefore int    `json:"rating_before"`
	RatingAfter  int    `json:"rating_after"`
	RatingChange int    `json:"rating_change"`
}

// UserProfileHandler handles the user profile page
func UserProfileHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/user_profile.html")
}

// GetUserProfileHandler returns user profile data as JSON
func GetUserProfileHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	targetUsername := vars["username"]

	if targetUsername == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		return
	}

	// Get user from database
	var user sql_service.User
	db := sql_service.DB()

	if err := db.Where("username = ?", targetUsername).First(&user).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Get user's solved problems (a submission counts as solved once it is "accepted").
	// Match both "accepted" and the legacy "ok" value for backwards compatibility.
	var submissions []sql_service.Submission
	db.Model(&sql_service.Submission{}).
		Where("username = ? AND status IN ?", targetUsername, []string{"accepted", "ok"}).
		Find(&submissions)

	// Get unique problem IDs
	solvedIDs := make(map[uint]bool)
	for _, sub := range submissions {
		solvedIDs[sub.ProblemID] = true
	}

	var solvedIDsList []uint
	for id := range solvedIDs {
		solvedIDsList = append(solvedIDsList, id)
	}

	// Get total submissions count
	var totalSub int64
	db.Model(&sql_service.Submission{}).Where("username = ?", targetUsername).Count(&totalSub)

	// Get accepted submissions count
	var acSub int64
	db.Model(&sql_service.Submission{}).Where("username = ? AND status IN ?", targetUsername, []string{"accepted", "ok"}).Count(&acSub)

	// Get contest history
	histories, _ := sql_service.GetUserRatingHistory(targetUsername)
	contests := make([]ContestHistoryEntry, 0, len(histories))
	for _, h := range histories {
		contests = append(contests, ContestHistoryEntry{
			ContestName:  h.ContestName,
			ContestID:    h.ContestID,
			Rank:         h.Rank,
			TotalScore:   h.TotalScore,
			RatingBefore: h.RatingBefore,
			RatingAfter:  h.RatingAfter,
			RatingChange: h.RatingChange,
		})
	}

	profile := UserProfile{
		Username:    user.Username,
		GroupName:   user.GroupName,
		Rating:      user.Rating,
		Role:        user.Role,
		SolvedCount: len(solvedIDsList),
		SolvedIDs:   solvedIDsList,
		TotalSub:    int(totalSub),
		ACSub:       int(acSub),
		CreatedAt:   user.CreatedAt.Format("2006-01-02 15:04:05"),
		Contests:    contests,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(profile)
}

// UpdateUserRatingHandler updates user's rating
func UpdateUserRatingHandler(w http.ResponseWriter, r *http.Request) {
	currentUsername := manage.CurrentUsername(r)
	if currentUsername == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	targetUsername := vars["username"]

	// Only allow users to update their own rating or admin
	if currentUsername != targetUsername && !manage.CheckUserPermission(currentUsername, "EditPermission") {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	var req struct {
		Rating int `json:"rating"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	db := sql_service.DB()
	if err := db.Model(&sql_service.User{}).Where("username = ?", targetUsername).Update("rating", req.Rating).Error; err != nil {
		http.Error(w, "Failed to update rating", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message":"Rating updated"}`))
}

// ChangePasswordHandler lets the currently authenticated user change their own
// password. The current password must be supplied and verified before the change
// is applied.
func ChangePasswordHandler(w http.ResponseWriter, r *http.Request) {
	currentUsername := manage.CurrentUsername(r)
	if currentUsername == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	if len(req.NewPassword) < 6 {
		http.Error(w, "new password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	if err := sql_service.ChangePassword(currentUsername, req.OldPassword, req.NewPassword); err != nil {
		// Distinguish "wrong current password" (401) from other errors.
		if err.Error() == "current password is incorrect" {
			http.Error(w, err.Error(), http.StatusUnauthorized)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	writeJSONMessage(w, "ok", "password updated")
}

// GetUserSubmissionsHandler returns user's submissions for a specific problem
func GetUserSubmissionsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	targetUsername := vars["username"]

	problemIDStr := r.URL.Query().Get("problem_id")

	var submissions []sql_service.Submission
	db := sql_service.DB()

	query := db.Where("username = ?", targetUsername)

	if problemIDStr != "" {
		if pid, err := strconv.ParseUint(problemIDStr, 10, 64); err == nil {
			query = query.Where("problem_id = ?", uint(pid))
		}
	}

	if err := query.Order("created_at DESC").Limit(50).Find(&submissions).Error; err != nil {
		http.Error(w, "Failed to fetch submissions", http.StatusInternalServerError)
		return
	}

	// Don't send code to client
	for i := range submissions {
		submissions[i].Code = ""
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(submissions)
}

// GetUserSolvedProblemsHandler returns a list of solved problem IDs for a user
func GetUserSolvedProblemsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	targetUsername := vars["username"]

	var submissions []sql_service.Submission
	db := sql_service.DB()

	if err := db.Where("username = ? AND status IN ?", targetUsername, []string{"accepted", "ok"}).Find(&submissions).Error; err != nil {
		http.Error(w, "Failed to fetch solved problems", http.StatusInternalServerError)
		return
	}

	solvedIDs := make(map[uint]bool)
	for _, sub := range submissions {
		solvedIDs[sub.ProblemID] = true
	}

	var solvedIDsList []uint
	for id := range solvedIDs {
		solvedIDsList = append(solvedIDsList, id)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"solved_ids": solvedIDsList,
		"count":      len(solvedIDsList),
	})
}

// GetUserBioHandler returns user's bio (markdown) as JSON
// Only the user themselves or users with EditPermission can view
func GetUserBioHandler(w http.ResponseWriter, r *http.Request) {
	currentUsername := manage.CurrentUsername(r)
	if currentUsername == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	targetUsername := vars["username"]

	// Only allow user themselves or users with EditPermission to view bio
	if currentUsername != targetUsername && !manage.CheckUserPermission(currentUsername, "EditPermission") {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	var user sql_service.User
	db := sql_service.DB()
	if err := db.Where("username = ?", targetUsername).First(&user).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"bio": user.Bio,
	})
}

// UpdateUserBioHandler updates user's bio (markdown)
// Only the user themselves or users with EditPermission can update
func UpdateUserBioHandler(w http.ResponseWriter, r *http.Request) {
	currentUsername := manage.CurrentUsername(r)
	if currentUsername == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	targetUsername := vars["username"]

	// Only allow user themselves or users with EditPermission to update bio
	if currentUsername != targetUsername && !manage.CheckUserPermission(currentUsername, "EditPermission") {
		http.Error(w, "Permission denied", http.StatusForbidden)
		return
	}

	var req struct {
		Bio string `json:"bio"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Security: limit bio length to 100KB
	const maxBioLength = 100 * 1024
	if len(req.Bio) > maxBioLength {
		http.Error(w, "bio exceeds maximum length of 100KB", http.StatusBadRequest)
		return
	}

	db := sql_service.DB()
	if err := db.Model(&sql_service.User{}).Where("username = ?", targetUsername).Update("bio", req.Bio).Error; err != nil {
		http.Error(w, "Failed to update bio", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"message":"Bio updated"}`))
}
