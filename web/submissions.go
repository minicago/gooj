package web

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/minicago/gooj/manage"
	"github.com/minicago/gooj/sql_service"
)

// SubmissionsHandler handles the submissions list page
func SubmissionsHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/submissions.html")
}

// SubmissionDetailHandler handles the submission detail page
func SubmissionDetailHandler(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/submission_detail.html")
}

// GetSubmissionsHandler returns paginated submissions
func GetSubmissionsHandler(w http.ResponseWriter, r *http.Request) {
	currentUsername := manage.CurrentUsername(r)
	if currentUsername == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get pagination parameters
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")
	problem := r.URL.Query().Get("problem")
	username := r.URL.Query().Get("username")

	page := 1
	limit := 20

	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	offset := (page - 1) * limit

	// Get submissions based on user permissions
	var submissions []sql_service.Submission
	var total int64
	db := sql_service.DB()

	query := db.Model(&sql_service.Submission{})

	// Filter by problem if specified
	if problem != "" {
		query = query.Where("problem = ?", problem)
	}

	// Filter by username if specified
	if username != "" {
		query = query.Where("username = ?", username)
	}

	// If user is not an edit admin, only show their own submissions
	if !manage.CheckUserPermission(currentUsername, "EditPermission") {
		query = query.Where("username = ?", currentUsername)
	}

	// Count total
	query.Count(&total)

	// Get paginated results with test results preloaded
	err := query.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Preload("TestResults").
		Find(&submissions).Error

	if err != nil {
		http.Error(w, "Failed to fetch submissions", http.StatusInternalServerError)
		return
	}

	// Prepare response
	response := map[string]interface{}{
		"total":       total,
		"page":        page,
		"limit":       limit,
		"submissions": submissions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// GetSubmissionHandler returns a single submission by ID
func GetSubmissionHandler(w http.ResponseWriter, r *http.Request) {
	currentUsername := manage.CurrentUsername(r)
	if currentUsername == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	idStr := vars["id"]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid submission ID", http.StatusBadRequest)
		return
	}

	// Get submission with test results
	var submission sql_service.Submission
	db := sql_service.DB()
	err = db.Preload("TestResults").First(&submission, id).Error

	if err != nil {
		http.Error(w, "Submission not found", http.StatusNotFound)
		return
	}

	// Check permissions: user can view their own submission or edit admins can view any
	if submission.Username != currentUsername && !manage.CheckUserPermission(currentUsername, "EditPermission") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(submission)
}

// GetProblemStatsHandler returns statistics for a problem
func GetProblemStatsHandler(w http.ResponseWriter, r *http.Request) {
	currentUsername := manage.CurrentUsername(r)
	if currentUsername == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	problem := r.URL.Query().Get("problem")
	if problem == "" {
		http.Error(w, "Problem name required", http.StatusBadRequest)
		return
	}

	db := sql_service.DB()

	// Get total number of users who passed this problem
	var passedCount int64
	err := db.Model(&sql_service.Submission{}).
		Where("problem = ? AND status = 'ok'", problem).
		Distinct("username").
		Count(&passedCount).Error

	if err != nil {
		http.Error(w, "Failed to get passed count", http.StatusInternalServerError)
		return
	}

	// Get current user's highest score for this problem
	var userBestScore int
	var userBestSubmission sql_service.Submission

	// For this system, we'll consider "ok" as 100 points, others as 0
	// You might want to adjust this based on your scoring system
	err = db.Where("username = ? AND problem = ?", currentUsername, problem).
		Order("CASE WHEN status = 'ok' THEN 1 ELSE 2 END, created_at DESC").
		First(&userBestSubmission).Error

	if err == nil && userBestSubmission.Status == "ok" {
		userBestScore = 100
	}

	// Get total submission count for this problem
	var totalSubmissions int64
	db.Model(&sql_service.Submission{}).
		Where("problem = ?", problem).
		Count(&totalSubmissions)

	response := map[string]interface{}{
		"problem":           problem,
		"passed_count":      passedCount,
		"user_best_score":   userBestScore,
		"total_submissions": totalSubmissions,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// RenderSubmissionDetail renders the submission detail page with template
func RenderSubmissionDetail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	tmpl, err := template.ParseFiles("static/submission_detail.html")
	if err != nil {
		http.Error(w, "Failed to load template", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"SubmissionID": id,
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}
