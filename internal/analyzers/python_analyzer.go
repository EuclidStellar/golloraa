package analyzers

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/euclidstellar/gollora/internal/models"
	"github.com/euclidstellar/gollora/internal/utils"
)

// PythonAnalyzer analyzes Python code using configured tools.
type PythonAnalyzer struct {
    tools []models.Tool
}

// NewPythonAnalyzer creates a new PythonAnalyzer.
func NewPythonAnalyzer(tools []models.Tool) *PythonAnalyzer {
    return &PythonAnalyzer{
        tools: tools,
    }
}

// Analyze runs the analysis for Python files.
func (a *PythonAnalyzer) Analyze(ctx context.Context, repoPath string, files []models.FileToAnalyze) ([]models.CodeIssue, error) {
    var allIssues []models.CodeIssue

    var pyFiles []models.FileToAnalyze
    for _, file := range files {
        if strings.HasSuffix(file.Path, ".py") {
            pyFiles = append(pyFiles, file)
        }
    }

    if len(pyFiles) == 0 {
        return nil, nil
    }

    utils.LogWithLocation(utils.Info, "Analyzing %d Python files", len(pyFiles))

    for _, tool := range a.tools {
        if !tool.Enabled {
            continue
        }

        utils.LogWithLocation(utils.Info, "Running tool: %s", tool.Name)

        switch tool.Name {
        case "flake8":
            issues, err := a.runFlake8(ctx, repoPath, tool)
            if err != nil {
                utils.LogWithLocation(utils.Error, "Error running flake8: %v", err)
                continue
            }
            allIssues = append(allIssues, issues...)
        }
    }

    return allIssues, nil
}

// runFlake8 executes the flake8 linter and parses its output.
func (a *PythonAnalyzer) runFlake8(ctx context.Context, repoPath string, tool models.Tool) ([]models.CodeIssue, error) {
    cmd := exec.CommandContext(ctx, tool.Command, tool.Args...)
    cmd.Dir = repoPath

    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    // flake8 exits with a non-zero status code if it finds issues, so we don't treat that as a fatal error.
    _ = cmd.Run()

    if stderr.Len() > 0 {
        utils.LogWithLocation(utils.Warn, "flake8 stderr: %s", stderr.String())
    }

    output := stdout.String()
    utils.LogWithLocation(utils.Debug, "flake8 output: %s", output)

    return a.parseFlake8Output(output)
}

// parseFlake8Output converts flake8's default output into a slice of CodeIssue.
func (a *PythonAnalyzer) parseFlake8Output(output string) ([]models.CodeIssue, error) {
    var issues []models.CodeIssue
    lines := strings.Split(output, "\n")

    for _, line := range lines {
        if strings.TrimSpace(line) == "" {
            continue
        }

        // Format: path:line:column: code message
        parts := strings.SplitN(line, ":", 4)
        if len(parts) < 4 {
            continue
        }

        filePath := strings.TrimPrefix(parts[0], "./")
        lineNum, _ := strconv.Atoi(parts[1])
        colNum, _ := strconv.Atoi(parts[2])
        message := strings.TrimSpace(parts[3])
        ruleID := strings.Split(message, " ")[0]

        issue := models.CodeIssue{
            Title:       a.createTitle(ruleID, message),
            Description: message,
            File:        filePath,
            Line:        lineNum,
            Column:      colNum,
            Tool:        "flake8",
            RuleID:      ruleID,
            Severity:    a.mapSeverity(ruleID),
            Type:        a.mapIssueType(ruleID),
        }
        issues = append(issues, issue)
    }

    utils.LogWithLocation(utils.Info, "Found %d issues from flake8", len(issues))
    return issues, nil
}

func (a *PythonAnalyzer) createTitle(ruleID, message string) string {
    title := fmt.Sprintf("Flake8 issue: %s", ruleID)
    if len(message) > 60 {
        return title
    }
    return fmt.Sprintf("%s: %s", title, message)
}

func (a *PythonAnalyzer) mapSeverity(ruleID string) models.IssueSeverity {
    switch {
    case strings.HasPrefix(ruleID, "F"): // PyFlakes (errors)
        return models.Error
    case strings.HasPrefix(ruleID, "E"): // pycodestyle (errors)
        return models.Error
    case strings.HasPrefix(ruleID, "W"): // pycodestyle (warnings)
        return models.Warning
    case strings.HasPrefix(ruleID, "C"): // mccabe (complexity)
        return models.Warning
    default:
        return models.Info
    }
}

func (a *PythonAnalyzer) mapIssueType(ruleID string) models.IssueType {
    switch {
    case strings.HasPrefix(ruleID, "F"):
        return models.Bug
    case strings.HasPrefix(ruleID, "E"):
        return models.CodeStyle
    case strings.HasPrefix(ruleID, "W"):
        return models.CodeStyle
    case strings.HasPrefix(ruleID, "C"):
        return models.Maintainability
    default:
        return models.CodeStyle
    }
}