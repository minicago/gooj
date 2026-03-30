package tuack

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ProcessStatement renders a tuack statement and handles special syntax
func ProcessStatement(statement string, problemDir string) (string, error) {
	// First, handle sample blocks that read from down directory
	statement = processSamples(statement, problemDir)

	// Mapping for Chinese section titles
	titles := map[string]string{
		"description":   "题目描述",
		"input format":  "输入格式",
		"output format": "输出格式",
		"sample":        "样例",
		"hints":         "提示",
		"hint":          "提示",
		"subtasks":      "子任务",
	}

	// Replace self.title() with actual title (just remove it) - handle spaces
	reTitle := regexp.MustCompile(`\{\{\s*self\s*\.\s*title\s*\(\s*\)\s*\}\}`)
	statement = reTitle.ReplaceAllString(statement, "")

	// Replace self.input_file() with standard text - handle spaces
	reInputFile := regexp.MustCompile(`\{\{\s*self\s*\.\s*input_file\s*\(\s*\)\s*\}\}`)
	statement = reInputFile.ReplaceAllString(statement, "（标准输入）")

	// Replace self.output_file() with standard text - handle spaces
	reOutputFile := regexp.MustCompile(`\{\{\s*self\s*\.\s*output_file\s*\(\s*\)\s*\}\}`)
	statement = reOutputFile.ReplaceAllString(statement, "（标准输出）")

	// Replace self.sample_text() - remove it - handle spaces
	reSampleText := regexp.MustCompile(`\{\{\s*self\s*\.\s*sample_text\s*\(\s*\)\s*\}\}`)
	statement = reSampleText.ReplaceAllString(statement, "")

	// Replace self.hint_text() - with standard hint section header - handle spaces
	reHintText := regexp.MustCompile(`\{\{\s*self\s*\.\s*hint_text\s*\(\s*\)\s*\}\}`)
	statement = reHintText.ReplaceAllString(statement, "### 提示\n\n")

	// Replace self.subtasks_text() - with standard subtasks section header - handle spaces
	reSubtasksText := regexp.MustCompile(`\{\{\s*self\s*\.\s*subtasks_text\s*\(\s*\)\s*\}\}`)
	statement = reSubtasksText.ReplaceAllString(statement, "### 子任务\n\n")

	// Handle s('...') pattern - replace with Chinese titles - handle spaces
	// This handles both s('hint'), s('subtasks'), s('sample', N), etc.
	re := regexp.MustCompile(`\{\{\s*s\s*\(\s*'([^']+)'\s*(?:,\s*\d+\s*)?\)\s*\}\}`)
	statement = re.ReplaceAllStringFunc(statement, func(match string) string {
		// Extract key
		matches := re.FindStringSubmatch(match)
		if len(matches) > 1 {
			key := strings.ToLower(matches[1])
			if val, ok := titles[key]; ok {
				return fmt.Sprintf("### %s\n\n", val)
			}
		}
		return match
	})

	// Clean up extra newlines
	statement = strings.TrimSpace(statement)
	statement = regexp.MustCompile(`\n{3,}`).ReplaceAllString(statement, "\n\n")

	return statement, nil
}

// processSamples replaces {{ s('sample', N) }} blocks with actual sample data
func processSamples(statement string, problemDir string) string {
	downDir := filepath.Join(problemDir, "down")

	// Pattern: {{ s('sample', N) }} with optional spaces, followed by content until next section
	re := regexp.MustCompile(`\{\{\s*s\s*\(\s*'sample'\s*,\s*(\d+)\s*\)\s*\}\}`)

	return re.ReplaceAllStringFunc(statement, func(match string) string {
		// Extract sample number
		matches := re.FindStringSubmatch(match)
		if len(matches) < 2 {
			return match
		}

		sampleNum := matches[1]

		// Read sample input and output from down directory
		inputFile := filepath.Join(downDir, fmt.Sprintf("%s.in", sampleNum))
		outputFile := filepath.Join(downDir, fmt.Sprintf("%s.ans", sampleNum))

		// Check if files exist
		inputData, inputErr := os.ReadFile(inputFile)
		outputData, outputErr := os.ReadFile(outputFile)

		var inputText, outputText string
		if inputErr == nil {
			inputText = string(inputData)
		} else {
			inputText = fmt.Sprintf("(样例输入文件不存在: %s)", inputFile)
		}
		if outputErr == nil {
			outputText = string(outputData)
		} else {
			outputText = fmt.Sprintf("(样例输出文件不存在: %s)", outputFile)
		}

		// Build sample block
		result := fmt.Sprintf("### 样例 %s\n\n**输入**\n```\n%s\n```\n\n**输出**\n```\n%s\n```\n\n",
			sampleNum, inputText, outputText)

		return result
	})
}
