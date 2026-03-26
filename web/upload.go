package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/minicago/gooj/manage"
	"github.com/minicago/gooj/tuack"
)

// UploadProblemHandler handles the upload of a tuack problem package
func UploadProblemHandler(w http.ResponseWriter, r *http.Request) {
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

	// Get problem name and title
	name := r.FormValue("name")
	title := r.FormValue("title")

	if name == "" || title == "" {
		http.Error(w, "Problem name and title are required", http.StatusBadRequest)
		return
	}

	// Validate name format (alphanumeric with underscores and hyphens)
	for _, ch := range name {
		if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-') {
			http.Error(w, "Problem name can only contain letters, numbers, underscores and hyphens", http.StatusBadRequest)
			return
		}
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
	tempDir, err := os.MkdirTemp("", "upload-*")
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

	// Import the tuack package
	result, err := tuack.ImportTuackPackage(zipPath, name, title)
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
