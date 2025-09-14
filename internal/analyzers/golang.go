package analyzers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/euclidstellar/gollora/internal/models"
	"github.com/euclidstellar/gollora/internal/utils"
)

type GolangAnalyzer struct {
    tools []models.Tool
}

func NewGolangAnalyzer(tools []models.Tool) *GolangAnalyzer {
    return &GolangAnalyzer{
        tools: tools,
    }
}

func (a *GolangAnalyzer) Analyze(ctx context.Context, repoPath string, files []models.FileToAnalyze) ([]models.CodeIssue, error) {
    var allIssues []models.CodeIssue

    var goFiles []models.FileToAnalyze
    for _, file := range files {
        if strings.HasSuffix(file.Path, ".go") {
            goFiles = append(goFiles, file)
        }
    }

    if len(goFiles) == 0 {
        return nil, nil
    }

    utils.LogWithLocation(utils.Info, "Analyzing %d Go files", len(goFiles))

    for _, tool := range a.tools {
        if !tool.Enabled {
            continue
        }

        utils.LogWithLocation(utils.Info, "Running tool: %s", tool.Name)
        
        switch tool.Name {
        case "golangci-lint":
            issues, err := a.runGolangCILint(ctx, repoPath, goFiles, tool)
            if err != nil {
                utils.LogWithLocation(utils.Error, "Error running golangci-lint: %v", err)
                continue
            }
            allIssues = append(allIssues, issues...)
        }
    }

    return allIssues, nil
}
func (a *GolangAnalyzer) runGolangCILint(ctx context.Context, repoPath string, files []models.FileToAnalyze, tool models.Tool) ([]models.CodeIssue, error) {
    goModPath := filepath.Join(repoPath, "go.mod")
    if _, err := os.Stat(goModPath); os.IsNotExist(err) {
        utils.LogWithLocation(utils.Info, "No go.mod file found, initializing temporary Go module")

        initCmd := exec.Command("go", "mod", "init", "temp")
        initCmd.Dir = repoPath
        if err := initCmd.Run(); err != nil {
            utils.LogWithLocation(utils.Warn, "Failed to initialize Go module: %v", err)
        }
    }

    var filePaths []string
    for _, file := range files {
        filePaths = append(filePaths, file.Path)
    }
    
    args := []string{"run", "-v"}
    
    if len(filePaths) > 0 {
        args = append(args, filePaths...)
    } else {
        var err error
        filePaths, err = findGoFiles(repoPath)
        if err != nil {
            return nil, fmt.Errorf("failed to find Go files: %v", err)
        }
        
        if len(filePaths) == 0 {
            return []models.CodeIssue{}, nil
        }
        
        args = append(args, filePaths...)
    }
    
    utils.LogWithLocation(utils.Info, "Running golangci-lint with args: %v", args)
    
    cmd := exec.CommandContext(ctx, tool.Command, args...)
    cmd.Dir = repoPath
    
    var stdout bytes.Buffer
    var stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    
    if err := cmd.Run(); err != nil {
        utils.LogWithLocation(utils.Warn, "Error running command: %v", err)
    }
    
    output := stdout.String() + "\n" + stderr.String()
    utils.LogWithLocation(utils.Info, "golangci-lint output: %s", output)

    var issues []models.CodeIssue
    lines := strings.Split(output, "\n")

    for _, line := range lines {
        if strings.HasPrefix(line, "INFO") || strings.TrimSpace(line) == "" {
            continue
        }
        
       
        if strings.Contains(line, ".go:") && strings.Contains(line, ": ") {
            parts := strings.SplitN(line, ":", 4)
            if len(parts) >= 4 {
                filePath := parts[0]
                lineNum, _ := strconv.Atoi(parts[1])
                colNum, _ := strconv.Atoi(parts[2])
                message := strings.TrimSpace(parts[3])
                
                linterName := "golangci-lint"
                if strings.HasSuffix(message, ")") {
                    if idx := strings.LastIndex(message, "("); idx != -1 {
                        linterName = strings.TrimSpace(message[idx+1 : len(message)-1])
                        message = strings.TrimSpace(message[:idx])
                    }
                }
            
                filePath = filepath.Base(filePath)

                severity := models.Error 
                issueType := a.mapIssueType(linterName, message)
                
                issues = append(issues, models.CodeIssue{
                    Title:       fmt.Sprintf("%s: %s", linterName, a.shortenDescription(message)),
                    Description: message,
                    File:        filePath,
                    Line:        lineNum,
                    Column:      colNum,
                    Severity:    severity,
                    Type:        issueType,
                    Tool:        "golangci-lint",
                    RuleID:      linterName,
                })
                
                utils.LogWithLocation(utils.Info, "Found issue: %s:%d:%d - %s (%s)", 
                    filePath, lineNum, colNum, message, linterName)
            }
        }
    }
    
    utils.LogWithLocation(utils.Info, "Found %d issues from golangci-lint", len(issues))
    return issues, nil
}

func findGoFiles(dir string) ([]string, error) {
    var files []string
    err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        if !info.IsDir() && strings.HasSuffix(path, ".go") {
            relPath, err := filepath.Rel(dir, path)
            if err != nil {
                return err
            }
            files = append(files, relPath)
        }
        return nil
    })
    return files, err
}

func (a *GolangAnalyzer) mapIssueType(linter, message string) models.IssueType {
    linter = strings.ToLower(linter)
    message = strings.ToLower(message)
    
    if linter == "gosec" || strings.Contains(message, "security") || strings.Contains(message, "vulnerability") {
        return models.Security
    }
    
    if strings.Contains(message, "performance") || strings.Contains(message, "efficient") {
        return models.Performance
    }
    
    if linter == "errcheck" || linter == "govet" || strings.Contains(message, "error") {
        return models.Bug
    }
    
    if linter == "staticcheck" || linter == "gosimple" || linter == "ineffassign" {
        return models.Maintainability
    }
    
    return models.CodeStyle
}

func (a *GolangAnalyzer) shortenDescription(description string) string {
    if len(description) <= 60 {
        return description
    }
    return description[:57] + "..."
}