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

	// Filter by problem if specified. The submission stores the numeric problem id.
	if problem != "" {
		problemID, err := parseProblemID(problem)
		if err != nil {
			http.Error(w, "invalid problem id", http.StatusBadRequest)
			return
		}
		query = query.Where("problem_id = ?", problemID)
	}

	// Filter by username if specified
	if username != "" {
		query = query.Where("username = ?", username)
	}

	// Permission-based visibility:
	// - Editors (teachers/admins) see everything.
	// - Other users see all submissions EXCEPT those belonging to a problem that is
	//   part of an *ongoing* contest and owned by someone else (contest code/results
	//   are private until the contest ends). After a contest ends, all submissions
	//   and code become public.
	canViewAll := manage.CheckUserPermission(currentUsername, "EditPermission")
	if !canViewAll {
		ongoing, err := sql_service.OngoingContestProblemIDs()
		if err == nil && len(ongoing) > 0 {
			ongoingIDs := make([]uint, 0, len(ongoing))
			for pid := range ongoing {
				ongoingIDs = append(ongoingIDs, pid)
			}
			query = query.Where("username = ? OR problem_id NOT IN ?", currentUsername, ongoingIDs)
		}
	}

	// Count total
	query.Count(&total)

	// Get paginated results with test results preloaded
	err := query.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		// Preload("TestResults").
		Find(&submissions).Error

	if err != nil {
		http.Error(w, "Failed to fetch submissions", http.StatusInternalServerError)
		return
	}

	//do not send code to user
	for i := range submissions {
		submissions[i].Code = ""
	}

	// Strip TestResults if TestVisible=false and user is not an editor. Note we keep
	// the (already blanked) code out of the list; only evaluation details are hidden.
	if !canViewAll && len(submissions) > 0 {
		// Batch-load TestVisible flags for the involved problems.
		problemIDs := make([]uint, 0, len(submissions))
		for _, s := range submissions {
			problemIDs = append(problemIDs, s.ProblemID)
		}
		var problems []sql_service.Problem
		db.Model(&sql_service.Problem{}).Where("id IN ?", problemIDs).Find(&problems)
		showMap := make(map[uint]bool, len(problems))
		for _, p := range problems {
			showMap[p.ID] = p.TestVisible
		}
		for i := range submissions {
			if !showMap[submissions[i].ProblemID] {
				submissions[i].TestResults = nil
				submissions[i].Score = 0
				submissions[i].Status = "unknown"
			}
		}
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

	// Permission check:
	// - The owner and editors (teachers/admins) can always view.
	// - Other users may view a submission only if its problem is NOT part of an
	//   ongoing contest (contest results/code are private until the contest ends).
	isOwner := submission.Username == currentUsername
	canViewAll := manage.CheckUserPermission(currentUsername, "EditPermission")
	if !isOwner && !canViewAll {
		inOngoing, err := sql_service.IsProblemInOngoingContest(submission.ProblemID)
		if err == nil && inOngoing {
			http.Error(w, "Forbidden during contest", http.StatusForbidden)
			return
		}
	}

	// Strip TestResults and Score if the problem has TestVisible=false and user is not an editor.
	// Code is still returned (it is public for non-contest problems), only the
	// evaluation details are hidden.
	if !canViewAll {
		var problem sql_service.Problem
		if err := db.First(&problem, submission.ProblemID).Error; err == nil && !problem.TestVisible {
			submission.TestResults = nil
			submission.Score = 0
			submission.Status = "unknown"
		}
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

	problemIDStr := r.URL.Query().Get("problem")
	problemID, err := parseProblemID(problemIDStr)
	if err != nil {
		http.Error(w, "invalid problem id", http.StatusBadRequest)
		return
	}

	db := sql_service.DB()

	// Load problem to check TestVisible
	var problem sql_service.Problem
	if err := db.First(&problem, problemID).Error; err != nil {
		http.Error(w, "problem not found", http.StatusNotFound)
		return
	}

	canViewEvaluation := manage.CheckUserPermission(currentUsername, "EditPermission") || problem.TestVisible
	if !canViewEvaluation {
		// Return zeroed evaluation data — do not leak any AC/score info
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"problem_id":        problemID,
			"passed_count":      0,
			"user_best_score":   0,
			"total_submissions": 0,
		})
		return
	}

	// Get total number of users who passed this problem
	var passedCount int64
	err = db.Model(&sql_service.Submission{}).
		Where("problem_id = ? AND status IN ?", problemID, []string{"accepted", "ok"}).
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
	err = db.Where("problem_id = ? AND username = ?", problemID, currentUsername).
		Order("score DESC").
		First(&userBestSubmission).Error

	if err == nil {
		userBestScore = userBestSubmission.Score
	} else {
		userBestScore = 0
	}

	// Get total submission count for this problem
	var totalSubmissions int64
	db.Model(&sql_service.Submission{}).
		Where("problem_id = ?", problemID).
		Count(&totalSubmissions)

	response := map[string]interface{}{
		"problem_id":        problemID,
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
