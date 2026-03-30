package tuack

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/minicago/gooj/sql_service"
	"gopkg.in/yaml.v3"
)

// TuackConfig represents the conf.yaml structure in a tuack project
type TuackConfig struct {
	Args    map[string]interface{} `yaml:"args"`
	Compile map[string]string      `yaml:"compile"`
	Data    []struct {
		Cases []interface{} `yaml:"cases"`
		Score float64       `yaml:"score"`
	} `yaml:"data"`
	// Other fields may exist but are optional
}

// ImportResult contains the result of importing a tuack package
type ImportResult struct {
	ProblemID uint   `json:"problem_id"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	Message   string `json:"message"`
}

// ImportTuackPackage imports a tuack package from a zip file
func ImportTuackPackage(zipPath, name, title string) (*ImportResult, error) {
	// 1. Open zip file
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip file: %v", err)
	}
	defer reader.Close()

	// 2. Extract to temporary directory
	tempDir, err := os.MkdirTemp("", "tuack-import-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Extract all files
	for _, file := range reader.File {
		filePath := filepath.Join(tempDir, file.Name)

		// Create directory structure
		if file.FileInfo().IsDir() {
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %v", file.Name, err)
		}

		// Extract file
		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return nil, fmt.Errorf("failed to create file %s: %v", file.Name, err)
		}

		srcFile, err := file.Open()
		if err != nil {
			dstFile.Close()
			return nil, fmt.Errorf("failed to open zip entry %s: %v", file.Name, err)
		}

		_, err = io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to extract file %s: %v", file.Name, err)
		}
	}

	// 3. Find the tuack project root directory
	tuackRoot := findTuackRoot(tempDir)
	if tuackRoot == "" {
		return nil, errors.New("could not find tuack project root in zip file")
	}

	// 4. Read and parse conf.yaml
	config, err := parseConfig(filepath.Join(tuackRoot, "conf.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse conf.yaml: %v", err)
	}

	// 5. Read and process statement
	rawStatement, err := readStatement(tuackRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read statement: %v", err)
	}

	// Process statement to handle templates and samples
	// We'll process it after creating problem directory
	statement := rawStatement

	// 6. Calculate total test count from data
	testCount := 0
	for _, group := range config.Data {
		for _, caseItem := range group.Cases {
			switch v := caseItem.(type) {
			case int:
				testCount++
			case float64:
				testCount++
			case string:
				// Try to parse as number
				if _, err := strconv.Atoi(v); err == nil {
					testCount++
				}
			}
		}
	}

	// 7. Create problem in database
	problem := sql_service.Problem{
		Name:        name,
		Title:       title,
		Description: statement,
		TestsCount:  testCount,
		TimeLimitMs: 1000, // Default time limit, can be extracted from config if available
		MemLimitMB:  512,  // Default memory limit
	}

	// Get next problem ID
	var lastProblem sql_service.Problem
	db := sql_service.DB()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	if err := db.Last(&lastProblem).Error; err != nil {
		// No problems yet, start from 1
		problem.ID = 1
	} else {
		problem.ID = lastProblem.ID + 1
	}

	// Create directory for problem
	problemDir := filepath.Join("data", "problem", strconv.FormatUint(uint64(problem.ID), 10))
	if err := os.MkdirAll(problemDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create problem directory: %v", err)
	}

	// 8. Copy down directory if exists (needed before processing statement for samples)
	downSrc := filepath.Join(tuackRoot, "down")
	if _, err := os.Stat(downSrc); err == nil {
		downDst := filepath.Join(problemDir, "down")
		if err := copyDirectory(downSrc, downDst); err != nil {
			return nil, fmt.Errorf("failed to copy down directory: %v", err)
		}
	}

	// 8. Process statement to handle templates and samples
	statement, err = ProcessStatement(statement, problemDir)
	if err != nil {
		return nil, fmt.Errorf("failed to process statement: %v", err)
	}

	// 9. Write statement.md
	statementPath := filepath.Join(problemDir, "statement.md")
	if err := os.WriteFile(statementPath, []byte(statement), 0644); err != nil {
		return nil, fmt.Errorf("failed to write statement.md: %v", err)
	}

	// 10. Copy test data
	if err := copyTestData(tuackRoot, problemDir, config); err != nil {
		return nil, fmt.Errorf("failed to copy test data: %v", err)
	}

	// 12. Create config.json with grouped test cases
	if err := createConfigJSON(problemDir, config, testCount); err != nil {
		return nil, fmt.Errorf("failed to create config.json: %v", err)
	}

	// 13. Save problem to database
	if err := db.Create(&problem).Error; err != nil {
		// Clean up created directory
		os.RemoveAll(problemDir)
		return nil, fmt.Errorf("failed to save problem to database: %v", err)
	}

	return &ImportResult{
		ProblemID: problem.ID,
		Name:      problem.Name,
		Title:     problem.Title,
		Message:   "Problem imported successfully",
	}, nil
}

// UpdateTuackPackage updates an existing problem from a tuack package zip file
func UpdateTuackPackage(zipPath string, problemID uint) (*ImportResult, error) {
	// 1. Open zip file
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open zip file: %v", err)
	}
	defer reader.Close()

	// 2. Extract to temporary directory
	tempDir, err := os.MkdirTemp("", "tuack-update-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Extract all files
	for _, file := range reader.File {
		filePath := filepath.Join(tempDir, file.Name)

		// Create directory structure
		if file.FileInfo().IsDir() {
			os.MkdirAll(filePath, os.ModePerm)
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(filePath), os.ModePerm); err != nil {
			return nil, fmt.Errorf("failed to create directory for %s: %v", file.Name, err)
		}

		// Extract file
		dstFile, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			return nil, fmt.Errorf("failed to create file %s: %v", file.Name, err)
		}

		srcFile, err := file.Open()
		if err != nil {
			dstFile.Close()
			return nil, fmt.Errorf("failed to open zip entry %s: %v", file.Name, err)
		}

		_, err = io.Copy(dstFile, srcFile)
		srcFile.Close()
		dstFile.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to extract file %s: %v", file.Name, err)
		}
	}

	// 3. Find the tuack project root directory
	tuackRoot := findTuackRoot(tempDir)
	if tuackRoot == "" {
		return nil, errors.New("could not find tuack project root in zip file")
	}

	// 4. Read and parse conf.yaml
	config, err := parseConfig(filepath.Join(tuackRoot, "conf.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse conf.yaml: %v", err)
	}

	// 5. Read and process statement
	rawStatement, err := readStatement(tuackRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read statement: %v", err)
	}

	// 6. Calculate total test count from data
	testCount := 0
	for _, group := range config.Data {
		for _, caseItem := range group.Cases {
			switch v := caseItem.(type) {
			case int:
				testCount++
			case float64:
				testCount++
			case string:
				// Try to parse as number
				if _, err := strconv.Atoi(v); err == nil {
					testCount++
				}
			}
		}
	}

	// Get existing problem from database
	db := sql_service.DB()
	if db == nil {
		return nil, errors.New("database not initialized")
	}

	var problem sql_service.Problem
	if err := db.First(&problem, problemID).Error; err != nil {
		return nil, fmt.Errorf("problem not found: %v", err)
	}

	// Get problem directory
	problemDir := filepath.Join("data", "problem", strconv.FormatUint(uint64(problem.ID), 10))

	// Remove old test data and down directory
	testsDir := filepath.Join(problemDir, "tests")
	downDir := filepath.Join(problemDir, "down")
	os.RemoveAll(testsDir)
	os.RemoveAll(downDir)

	// 7. Copy down directory if exists (needed before processing statement for samples)
	downSrc := filepath.Join(tuackRoot, "down")
	if _, err := os.Stat(downSrc); err == nil {
		downDst := filepath.Join(problemDir, "down")
		if err := copyDirectory(downSrc, downDst); err != nil {
			return nil, fmt.Errorf("failed to copy down directory: %v", err)
		}
	}

	// 8. Process statement to handle templates and samples
	statement, err := ProcessStatement(rawStatement, problemDir)
	if err != nil {
		return nil, fmt.Errorf("failed to process statement: %v", err)
	}

	// 9. Write statement.md
	statementPath := filepath.Join(problemDir, "statement.md")
	if err := os.WriteFile(statementPath, []byte(statement), 0644); err != nil {
		return nil, fmt.Errorf("failed to write statement.md: %v", err)
	}

	// 10. Copy test data
	if err := copyTestData(tuackRoot, problemDir, config); err != nil {
		return nil, fmt.Errorf("failed to copy test data: %v", err)
	}

	// 11. Create config.json with grouped test cases
	if err := createConfigJSON(problemDir, config, testCount); err != nil {
		return nil, fmt.Errorf("failed to create config.json: %v", err)
	}

	// 12. Update problem in database
	problem.Description = statement
	problem.TestsCount = testCount

	if err := db.Save(&problem).Error; err != nil {
		return nil, fmt.Errorf("failed to update problem in database: %v", err)
	}

	return &ImportResult{
		ProblemID: problem.ID,
		Name:      problem.Name,
		Title:     problem.Title,
		Message:   "Problem updated successfully",
	}, nil
}

func parseConfig(configPath string) (*TuackConfig, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config TuackConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func readStatement(baseDir string) (string, error) {
	// Try to find statement file
	statementDir := filepath.Join(baseDir, "statement")

	// Check for zh-cn.md (Chinese statement)
	zhCNPath := filepath.Join(statementDir, "zh-cn.md")
	if data, err := os.ReadFile(zhCNPath); err == nil {
		return string(data), nil
	}

	// Check for en.md (English statement)
	enPath := filepath.Join(statementDir, "en.md")
	if data, err := os.ReadFile(enPath); err == nil {
		return string(data), nil
	}

	// Check for any .md file in statement directory
	files, err := os.ReadDir(statementDir)
	if err != nil {
		return "", err
	}

	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".md") {
			data, err := os.ReadFile(filepath.Join(statementDir, file.Name()))
			if err == nil {
				return string(data), nil
			}
		}
	}

	return "", errors.New("no statement file found")
}

func copyTestData(srcBase, dstBase string, config *TuackConfig) error {
	srcDataDir := filepath.Join(srcBase, "data")
	dstDataDir := dstBase // Test data goes directly to problem directory

	// Collect all test case numbers
	testCases := make(map[int]bool)
	for _, group := range config.Data {
		for _, caseItem := range group.Cases {
			var caseNum int
			switch v := caseItem.(type) {
			case int:
				caseNum = v
			case float64:
				caseNum = int(v)
			case string:
				if n, err := strconv.Atoi(v); err == nil {
					caseNum = n
				} else {
					continue
				}
			default:
				continue
			}
			testCases[caseNum] = true
		}
	}

	// Copy test files
	for testNum := range testCases {
		inFile := fmt.Sprintf("%d.in", testNum)
		ansFile := fmt.Sprintf("%d.ans", testNum)

		srcIn := filepath.Join(srcDataDir, inFile)
		dstIn := filepath.Join(dstDataDir, inFile)

		srcAns := filepath.Join(srcDataDir, ansFile)
		dstAns := filepath.Join(dstDataDir, ansFile)

		// Copy input file
		if data, err := os.ReadFile(srcIn); err == nil {
			if err := os.WriteFile(dstIn, data, 0644); err != nil {
				return fmt.Errorf("failed to copy %s: %v", inFile, err)
			}
		}

		// Copy answer file
		if data, err := os.ReadFile(srcAns); err == nil {
			if err := os.WriteFile(dstAns, data, 0644); err != nil {
				return fmt.Errorf("failed to copy %s: %v", ansFile, err)
			}
		}
	}

	return nil
}

func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}

func createConfigJSON(problemDir string, config *TuackConfig, testCount int) error {
	// Convert tuack config to our config format with grouped test cases
	testGroups := make([]map[string]interface{}, 0, len(config.Data))

	for _, group := range config.Data {
		// Convert case items to integers
		caseNumbers := make([]int, 0, len(group.Cases))
		for _, caseItem := range group.Cases {
			switch v := caseItem.(type) {
			case int:
				caseNumbers = append(caseNumbers, v)
			case float64:
				caseNumbers = append(caseNumbers, int(v))
			case string:
				if n, err := strconv.Atoi(v); err == nil {
					caseNumbers = append(caseNumbers, n)
				}
			}
		}

		testGroup := map[string]interface{}{
			"cases": caseNumbers,
			"score": group.Score,
		}
		testGroups = append(testGroups, testGroup)
	}

	configJSON := map[string]interface{}{
		"test_cases":   testGroups,
		"time_limit":   1,   // Default 1 second
		"memory_limit": 512, // Default 512 MB
	}

	// Try to extract time limit from compile options if available
	if cppOpts, ok := config.Compile["cpp"]; ok {
		if strings.Contains(cppOpts, "-O2") {
			// Could parse more specific options
		}
	}

	data, err := json.MarshalIndent(configJSON, "", "    ")
	if err != nil {
		return err
	}

	configPath := filepath.Join(problemDir, "config.json")
	return os.WriteFile(configPath, data, 0644)
}

// findTuackRoot searches for the tuack project root directory
// It looks for a directory containing conf.yaml
func findTuackRoot(baseDir string) string {
	// First check if conf.yaml exists in baseDir
	if _, err := os.Stat(filepath.Join(baseDir, "conf.yaml")); err == nil {
		return baseDir
	}

	// Look for directories that might contain the tuack project
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(baseDir, entry.Name())
			// Check if this subdirectory contains conf.yaml
			if _, err := os.Stat(filepath.Join(subDir, "conf.yaml")); err == nil {
				return subDir
			}

			// Recursively search deeper (but limit depth to avoid infinite loops)
			if subResult := findTuackRoot(subDir); subResult != "" {
				return subResult
			}
		}
	}

	return ""
}
