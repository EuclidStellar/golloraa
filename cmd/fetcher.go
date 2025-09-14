package main

import (
	"context"
	"fmt"
	"os/exec"
	"os"
	"path/filepath"
	"strings"

	"github.com/euclidstellar/gollora/internal/models"
	"github.com/euclidstellar/gollora/internal/utils"
)

type CodeFetcher struct {}

func NewCodeFetcher() *CodeFetcher {
    return &CodeFetcher{}
}

func (cf *CodeFetcher) FetchCode(ctx context.Context, event models.WebhookEvent) (string, []models.FileToAnalyze, error) {

    tempDir, err := os.MkdirTemp("", "gollora-repo-")
    if err != nil {
        return "", nil, fmt.Errorf("failed to create temporary directory: %v", err)
    }
    
    utils.LogWithLocation(utils.Info, "Cloning repository: %s", event.RepoURL)
    
    branch := event.Branch
    err = utils.CloneRepository(tempDir, event.RepoURL, branch)
    if err != nil {
        os.RemoveAll(tempDir) 
        return "", nil, fmt.Errorf("failed to clone repository: %v", err)
    }

    if event.HeadCommit != "" {
        utils.LogWithLocation(utils.Info, "Checking out commit: %s", event.HeadCommit)
        err = utils.CheckoutCommit(tempDir, event.HeadCommit)
        if err != nil {
            utils.LogWithLocation(utils.Warn, "Failed to checkout commit %s: %v", event.HeadCommit, err)
           
        }
    }
    
    var changedFiles []string
    if len(event.ChangedFiles) > 0 {
        changedFiles = event.ChangedFiles
    } else {
        baseCommit := event.BaseCommit
        headCommit := event.HeadCommit

        if baseCommit == "" {
            baseCommit = "HEAD~1"
            utils.LogWithLocation(utils.Info, "Base commit not provided, using %s instead", baseCommit)
        }
        
        if headCommit == "" {
            headCommit = "HEAD"
            utils.LogWithLocation(utils.Info, "Head commit not provided, using %s instead", headCommit)
        }
        
        utils.LogWithLocation(utils.Info, "Getting changed files between %s and %s", baseCommit, headCommit)
        
      
        if baseCommit != "HEAD~1" {
            fetchCmd := exec.Command("git", "fetch", "origin", baseCommit)
            fetchCmd.Dir = tempDir
            fetchCmd.Run() 
        }
        
        if headCommit != "HEAD" {
            fetchCmd := exec.Command("git", "fetch", "origin", headCommit)
            fetchCmd.Dir = tempDir
            fetchCmd.Run() 
        }

        files, err := utils.GetChangedFiles(tempDir, baseCommit, headCommit)
        if err != nil {

            utils.LogWithLocation(utils.Warn, "Failed to get changed files: %v. Analyzing all files instead.", err)
 
            findCmd := exec.Command("find", ".", "-type", "f", "-not", "-path", "*/\\.*")
            findCmd.Dir = tempDir
            output, findErr := findCmd.Output()
            if findErr != nil {
                os.RemoveAll(tempDir) 
                return "", nil, fmt.Errorf("failed to list all files: %v", findErr)
            }
            
            allFiles := strings.Split(strings.TrimSpace(string(output)), "\n")
            for _, file := range allFiles {
                if file == "" || strings.HasPrefix(file, "./.git/") {
                    continue
                }
                file = strings.TrimPrefix(file, "./")
                changedFiles = append(changedFiles, file)
            }
        } else {
            changedFiles = files
        }
    }
    
    utils.LogWithLocation(utils.Info, "Found %d changed files", len(changedFiles))
    

    var filesToAnalyze []models.FileToAnalyze
    for _, file := range changedFiles {
        if shouldSkipFile(file, tempDir) {
            utils.LogWithLocation(utils.Debug, "Skipping file: %s", file)
            continue
        }
        
        filePath := filepath.Join(tempDir, file)
        content, err := os.ReadFile(filePath)
        if err != nil {
            utils.LogWithLocation(utils.Warn, "Failed to read file %s: %v", file, err)
            continue
        }
        
        language := determineLanguage(file)
        
        fileToAnalyze := models.FileToAnalyze{
            Path:     file,
            Content:  string(content),
            Language: language,
        }
        
        filesToAnalyze = append(filesToAnalyze, fileToAnalyze)
    }
    
    return tempDir, filesToAnalyze, nil
}

func shouldSkipFile(file string, tempDir string) bool {
    if strings.Contains(file , ".git/"){
        return true 
    }
    ext := strings.ToLower(filepath.Ext(file))
    binaryExtensions := []string{
        ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".ico", ".svg",
        ".pdf", ".zip", ".tar", ".gz", ".rar", ".7z",
        ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx",
        ".bin", ".exe", ".dll", ".so", ".dylib",
        ".class", ".jar", ".war", ".ear",
    }
    
    for _, binExt := range binaryExtensions {
        if ext == binExt {
            return true
        }
    }

    
    filePath := filepath.Join(tempDir, file)
    fileInfo, err := os.Stat(filePath)
    if err == nil && fileInfo.Size() > 1024*1024 {
        return true
    }
    
    return false
}

func determineLanguage(filename string) string {
    ext := strings.ToLower(filepath.Ext(filename))
    
    switch ext {
    case ".go":
        return "go"
    case ".js":
        return "javascript"
    case ".ts":
        return "typescript"
    case ".jsx":
        return "javascript"
    case ".tsx":
        return "typescript"
    case ".py":
        return "python"
    case ".java":
        return "java"
    case ".rb":
        return "ruby"
    case ".php":
        return "php"
    case ".c", ".h":
        return "c"
    case ".cpp", ".hpp", ".cc":
        return "cpp"
    case ".cs":
        return "csharp"
    case ".rs":
        return "rust"
    case ".swift":
        return "swift"
    case ".kt", ".kts":
        return "kotlin"
    case ".sh":
        return "shell"
    case ".yaml", ".yml":
        return "yaml"
    case ".json":
        return "json"
    case ".md":
        return "markdown"
    case ".html", ".htm":
        return "html"
    case ".css":
        return "css"
    case ".sql":
        return "sql"
    case ".dart":
        return "dart"
    default:
        return "unknown"
    }
}