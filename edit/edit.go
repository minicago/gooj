package edit

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

func ModifyProblemStatementHandler(w http.ResponseWriter, r *http.Request) {
	type reqBody struct {
		ProblemID    string `json:"problem_id"`
		NewStatement string `json:"new_statement"`
	}
	var req reqBody
	_ = json.NewDecoder(r.Body).Decode(&req)
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
