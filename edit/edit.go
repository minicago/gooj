package edit

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/minicago/gooj/manage"
	"github.com/minicago/gooj/sql_service"
	"github.com/minicago/gooj/tuack"
)

func ModifyProblemStatementHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		ProblemID    string `json:"problem_id"`
		NewStatement string `json:"new_statement"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if !manage.CheckUserPermission(manage.CurrentUsername(r), "EditPermission") {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}
	if req.ProblemID == "" || req.NewStatement == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	statementPath := filepath.Join("data", "problem", req.ProblemID, "statement.md")
	if err := os.WriteFile(statementPath, []byte(req.NewStatement), 0644); err != nil {
		http.Error(w, "failed to modify statement", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func AddTestDataHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		ProblemID  string `json:"problem_id"`
		TestIndex  int    `json:"test_index"`
		InputData  string `json:"input_data"`
		OutputData string `json:"output_data"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.ProblemID == "" || req.TestIndex <= 0 || req.InputData == "" || req.OutputData == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	inputPath := filepath.Join("data", "problem", req.ProblemID, filepath.Join("tests", fmt.Sprintf("%d.in", req.TestIndex)))
	outputPath := filepath.Join("data", "problem", req.ProblemID, filepath.Join("tests", fmt.Sprintf("%d.out", req.TestIndex)))
	if err := os.WriteFile(inputPath, []byte(req.InputData), 0644); err != nil {
		http.Error(w, "failed to add input data", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(outputPath, []byte(req.OutputData), 0644); err != nil {
		http.Error(w, "failed to add output data", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ImportTuackHandler handles importing a tuack package from a zip file
func ImportTuackHandler(w http.ResponseWriter, r *http.Request) {
	// Check if user has edit permission
	currentUser := manage.CurrentUsername(r)
	if !manage.CheckUserPermission(currentUser, "EditPermission") {
		http.Error(w, "permission denied", http.StatusForbidden)
		return
	}

	// Parse multipart form with 100MB max memory
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse form: %v", err), http.StatusBadRequest)
		return
	}

	// Get problem ID from form
	problemID := r.FormValue("problem_id")
	if problemID == "" {
		http.Error(w, "problem_id is required", http.StatusBadRequest)
		return
	}

	// Get uploaded file
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get uploaded file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file type
	if filepath.Ext(header.Filename) != ".zip" {
		http.Error(w, "Only .zip files are allowed", http.StatusBadRequest)
		return
	}

	// Create temporary file to save the zip
	tempDir, err := os.MkdirTemp("", "tuack-import-*")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create temp directory: %v", err), http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	zipPath := filepath.Join(tempDir, "problem.zip")
	dst, err := os.Create(zipPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create temp file: %v", err), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	// Copy uploaded file to temp location
	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save uploaded file: %v", err), http.StatusInternalServerError)
		return
	}

	// Import the tuack package using existing function
	// Note: ImportTuackPackage expects name and title parameters, but we're importing to an existing problem
	// We need to get the existing problem's name and title from the database
	db := sql_service.DB()
	if db == nil {
		http.Error(w, "database not initialized", http.StatusInternalServerError)
		return
	}

	var problem sql_service.Problem
	if err := db.First(&problem, problemID).Error; err != nil {
		http.Error(w, fmt.Sprintf("Problem not found: %v", err), http.StatusNotFound)
		return
	}

	// Use existing problem name and title
	result, err := tuack.ImportTuackPackage(zipPath, problem.Name, problem.Title)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to import tuack package: %v", err), http.StatusInternalServerError)
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "success",
		"problem_id": result.ProblemID,
		"name":       result.Name,
		"title":      result.Title,
		"message":    result.Message,
	})
}
