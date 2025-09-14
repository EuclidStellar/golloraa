package utils

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"

	//"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type GitCloneOptions struct {
    URL       string
    Branch    string
    Commit    string
    Directory string
    Depth     int
    Timeout   time.Duration
}

func CloneRepository(dir, url, branch string) error {
    var cmd *exec.Cmd
    
    if branch != "" && branch != "main" && branch != "master" {
        cmd = exec.Command("git", "clone", "--single-branch", "--branch", branch, url, dir)
    } else {
        cmd = exec.Command("git", "clone", url, dir)
    }
    
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    
    return cmd.Run()
}

func CheckoutCommit(repoPath, commit string) error {
    cmd := exec.Command("git", "checkout", commit)
    cmd.Dir = repoPath
    
    var stderr bytes.Buffer
    cmd.Stderr = &stderr
    
    err := cmd.Run()
    if err != nil {
        return fmt.Errorf("failed to checkout commit: %v, stderr: %s", err, stderr.String())
    }
    
    return nil
}

func GetChangedFiles(repoPath, baseCommit, headCommit string) ([]string, error) {

    if baseCommit == "" || baseCommit == "0000000000000000000000000000000000000000" {
        cmd := exec.Command("git", "ls-tree", "-r", "--name-only", headCommit)
        cmd.Dir = repoPath
        
        output, err := cmd.Output()
        if err != nil {
            return nil, fmt.Errorf("failed to list files in commit %s: %v", headCommit, err)
        }
        
        files := strings.Split(strings.TrimSpace(string(output)), "\n")
        var validFiles []string
        
        for _, file := range files {
            if file == "" {
                continue
            }
            
            filePath := filepath.Join(repoPath, file)
            if _, err := os.Stat(filePath); err == nil {
                validFiles = append(validFiles, file)
            }
        }
        
        return validFiles, nil
    }

    cmd := exec.Command("git", "diff", "--name-only", baseCommit, headCommit)
    cmd.Dir = repoPath
    
    var stderr bytes.Buffer
    cmd.Stderr = &stderr
    
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("failed to get changed files: %v, stderr: %s", err, stderr.String())
    }
    
    files := strings.Split(strings.TrimSpace(string(output)), "\n")
    var validFiles []string
    
    for _, file := range files {
        if file == "" {
            continue
        }

        filePath := filepath.Join(repoPath, file)
        if _, err := os.Stat(filePath); err == nil {
            validFiles = append(validFiles, file)
        }
    }
    
    return validFiles, nil
}

// GetRepoStateHash determines the state of a repository for caching purposes.
// It first tries to get the latest Git commit hash. If that fails, it creates a
// hash based on the file paths and modification times in the directory.
func GetRepoStateHash(repoPath string) (string, error) {
    // First, try to use Git for the most accurate state.
    gitDir := filepath.Join(repoPath, ".git")
    if _, err := os.Stat(gitDir); err == nil {
        // .git directory exists, try to get commit hash
        commitHash, err := GetLatestCommitHash(repoPath)
        if err == nil {
            return commitHash, nil
        }
        // Log the error but fall back to file hashing
        LogWithLocation(Warn, "Could not get git commit hash, falling back to file-based hashing: %v", err)
    }

    // Fallback: Hash file paths and modification times.
    LogWithLocation(Info, "Not a git repository or git command failed. Using file-based hashing for cache state.")
    hasher := sha256.New()
    err := filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        // Ignore directories and the .git folder if it exists
        if info.IsDir() || strings.Contains(path, ".git"+string(filepath.Separator)) {
            return nil
        }

        relPath, err := filepath.Rel(repoPath, path)
        if err != nil {
            return err
        }

        // Add file path and modification time to the hash
        hasher.Write([]byte(relPath))
        hasher.Write([]byte(info.ModTime().String()))
        return nil
    })

    if err != nil {
        return "", fmt.Errorf("failed to walk repository for hashing: %v", err)
    }

    return hex.EncodeToString(hasher.Sum(nil)), nil
}

// GetLatestCommitHash returns the latest commit hash (HEAD) of a repository.
func GetLatestCommitHash(repoPath string) (string, error) {
    cmd := exec.Command("git", "rev-parse", "HEAD")
    cmd.Dir = repoPath
    
    output, err := cmd.Output()
    if err != nil {
        return "", fmt.Errorf("failed to get latest commit hash: %v", err)
    }
    
    return strings.TrimSpace(string(output)), nil
}

func GetFileContent(repoPath, filePath, commit string) ([]byte, error) {
    cmd := exec.Command("git", "show", fmt.Sprintf("%s:%s", commit, filePath))
    cmd.Dir = repoPath
    
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("failed to get file content: %v", err)
    }
    
    return output, nil
}

func DetectFileLanguage(filePath string) string {
    ext := strings.ToLower(filepath.Ext(filePath))
    
    switch ext {
    case ".go":
        return "go"
    case ".js":
        return "javascript"
    case ".ts":
        return "typescript"
    case ".jsx", ".tsx":
        return "react"
    case ".java":
        return "java"
    case ".py":
        return "python"
    case ".rb":
        return "ruby"
    case ".php":
        return "php"
    case ".c", ".h":
        return "c"
    case ".cpp", ".hpp", ".cc", ".hh":
        return "cpp"
    case ".cs":
        return "csharp"
    case ".swift":
        return "swift"
    case ".kt", ".kts":
        return "kotlin"
    case ".rs":
        return "rust"
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
    case ".scss", ".sass":
        return "sass"
    case ".xml":
        return "xml"
    case ".sh", ".bash":
        return "shell"
    default:
        return "unknown"
    }
}