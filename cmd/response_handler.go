package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/euclidstellar/gollora/internal/models"
	"github.com/euclidstellar/gollora/internal/utils"
)

type ResponseHandler struct {
    config     *models.Config
    aggregator *ResultAggregator
}

func NewResponseHandler(config *models.Config) *ResponseHandler {
    return &ResponseHandler{
        config:     config,
        aggregator: NewResultAggregator(config),
    }
}

func (rh *ResponseHandler) SendResponse(ctx context.Context, result *models.AnalysisResult) error {
    result = rh.aggregator.AggregateResults(result)

    if result.Event.Type == "pull_request" && result.Event.PullRequestURL != "" {
        return rh.sendPullRequestComments(ctx, result)
    }

    utils.LogWithLocation(utils.Info, "Analysis complete for push event. Results available at %s", 
        result.OutputFiles[0].Path)
    
    return nil
}
func (rh *ResponseHandler) sendPullRequestComments(ctx context.Context, result *models.AnalysisResult) error {
    switch result.Event.Provider {
    case "github":
        return rh.sendGitHubComments(ctx, result)
    default:
        return fmt.Errorf("unsupported VCS provider: %s", result.Event.Provider)
    }
}

func (rh *ResponseHandler) sendGitHubComments(ctx context.Context, result *models.AnalysisResult) error {
    if rh.config.GitHub.APIToken == "" {
        return fmt.Errorf("GitHub API token not configured")
    }
  
    repoParts := strings.Split(result.Event.RepoFullName, "/")
    if len(repoParts) != 2 {
        return fmt.Errorf("invalid repository full name: %s", result.Event.RepoFullName)
    }
    
    owner := repoParts[0]
    repo := repoParts[1]
    prNumber := result.Event.PullRequestID

    issuesToComment := rh.aggregator.FilterIssuesByThreshold(result, "warning")

    for i := range issuesToComment {
        issuesToComment[i].Event = result.Event 
    }

    if len(issuesToComment) == 0 {
        utils.LogWithLocation(utils.Info, "No issues found that meet the threshold")
        summaryComment := "## 🎉 Code Review Results\n\nNo issues found that meet the reporting threshold. Good job!"

        summaryURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, prNumber)
        if err := rh.postGitHubComment(ctx, summaryURL, summaryComment); err != nil {
            utils.LogWithLocation(utils.Error, "Failed to post summary comment: %v", err)
            return err
        }
        
        return nil
    }

    summaryComment := rh.formatSummaryComment(result)

    summaryURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, prNumber)
    if err := rh.postGitHubComment(ctx, summaryURL, summaryComment); err != nil {
        utils.LogWithLocation(utils.Error, "Failed to post summary comment: %v", err)

    }
    

    reviewURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/reviews", owner, repo, prNumber)
    err := rh.postGitHubReviewComments(ctx, reviewURL, issuesToComment, result.Event.HeadCommit)
    
    if err != nil {
        // If line comments fail, try posting a consolidated comment
        utils.LogWithLocation(utils.Warn, "Failed to post line comments: %v. Posting consolidated comment instead.", err)
        
        // Create a consolidated comment with all issues
        var consolidatedComment strings.Builder
        consolidatedComment.WriteString("## 🔍 Detailed Code Issues\n\n")
        
        for _, issue := range issuesToComment {
            consolidatedComment.WriteString(fmt.Sprintf("### %s in `%s` (line %d)\n\n", 
                issue.Title, issue.File, issue.Line))
            consolidatedComment.WriteString(issue.Description + "\n\n")
            if issue.Fix != "" {
                consolidatedComment.WriteString("**Suggested Fix:**\n\n```\n" + issue.Fix + "\n```\n\n")
            }
            consolidatedComment.WriteString("---\n\n")
        }
        
        if err := rh.postGitHubComment(ctx, summaryURL, consolidatedComment.String()); err != nil {
            utils.LogWithLocation(utils.Error, "Failed to post consolidated comment: %v", err)
            return err
        }
    }
    
    utils.LogWithLocation(utils.Info, "Successfully sent %d comments to GitHub PR #%d", len(issuesToComment)+1, prNumber)
    
    return nil
}

// postGitHubComment posts a comment to a GitHub PR
func (rh *ResponseHandler) postGitHubComment(ctx context.Context, url, body string) error {
    payload := map[string]string{"body": body}
    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("failed to marshal comment payload: %v", err)
    }
    
    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
    if err != nil {
        return fmt.Errorf("failed to create HTTP request: %v", err)
    }
    
    req.Header.Set("Authorization", "token "+rh.config.GitHub.APIToken)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "application/vnd.github.v3+json")
    
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("failed to send HTTP request: %v", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode >= 400 {
        return fmt.Errorf("GitHub API returned error: %s", resp.Status)
    }
    
    return nil
}

func (rh *ResponseHandler) postGitHubReviewComments(ctx context.Context, url string, issues []models.CodeIssue, commitSHA string) error {
   
    if len(issues) == 0 {
        return nil
    }
    
    repoParts := strings.Split(issues[0].Event.RepoFullName, "/")
    if len(repoParts) != 2 {
        return fmt.Errorf("invalid repository full name: %s", issues[0].Event.RepoFullName)
    }
    
    owner := repoParts[0]
    repo := repoParts[1]
    prNumber := issues[0].Event.PullRequestID
    
    validPaths, err := rh.parseGitHubPRDiff(ctx, owner, repo, prNumber)
    if err != nil {
        return fmt.Errorf("failed to parse PR diff: %v", err)
    }
    
    if len(validPaths) == 0 {
        return fmt.Errorf("no valid paths found in PR diff")
    }
    
    var validIssues []models.CodeIssue
    
    for _, issue := range issues {
        if validPaths[issue.File] != nil {
           
            originalLine := issue.Line
            found := false
         
            if validPaths[issue.File][originalLine] {
                validIssues = append(validIssues, issue)
                found = true
                continue
            }
            for offset := 1; offset <= 3 && !found; offset++ {
                if validPaths[issue.File][originalLine+offset] {
                    issue.Line = originalLine + offset
                    validIssues = append(validIssues, issue)
                    found = true
                    break
                }
                
                if validPaths[issue.File][originalLine-offset] {
                    issue.Line = originalLine - offset
                    validIssues = append(validIssues, issue)
                    found = true
                    break
                }
            }
           
            if !found {
                var firstLine int
                for line := range validPaths[issue.File] {
                    if firstLine == 0 || line < firstLine {
                        firstLine = line
                    }
                }
                
                if firstLine > 0 {
                    issue.Line = firstLine
                    validIssues = append(validIssues, issue)
                }
            }
        }
    }
 
    if len(validIssues) == 0 {
        return fmt.Errorf("no issues with valid file paths and line numbers found")
    }
   
    commentsByFile := make(map[string][]models.CodeIssue)
    for _, issue := range validIssues {
        commentsByFile[issue.File] = append(commentsByFile[issue.File], issue)
    }
    
    var comments []map[string]interface{}
    
    for filePath, fileIssues := range commentsByFile {
        for _, issue := range fileIssues {
            // Create comment
            comment := map[string]interface{}{
                "path": filePath,
                "line": issue.Line,
                "body": rh.formatIssueComment(issue),
                "side": "RIGHT",  
            }
            comments = append(comments, comment)
        }
    }
    
    if len(comments) == 0 {
        return nil
    }
    
    commentBytes, _ := json.Marshal(comments)
    utils.LogWithLocation(utils.Debug, "Sending review comments: %s", string(commentBytes))
    
    payload := map[string]interface{}{
        "commit_id": commitSHA,
        "event":     "COMMENT",
        "comments":  comments,
    }
    
    payloadBytes, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("failed to marshal review payload: %v", err)
    }
    
    req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(payloadBytes))
    if err != nil {
        return fmt.Errorf("failed to create HTTP request: %v", err)
    }
    
    req.Header.Set("Authorization", "token "+rh.config.GitHub.APIToken)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "application/vnd.github.v3+json")
    
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("failed to send HTTP request: %v", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode >= 400 {
        bodyBytes, _ := io.ReadAll(resp.Body)
        return fmt.Errorf("GitHub API returned error: %s, body: %s", resp.Status, string(bodyBytes))
    }
    
    return nil
}

func (rh *ResponseHandler) formatSummaryComment(result *models.AnalysisResult) string {
    var sb strings.Builder
    
    sb.WriteString("# :robot: Gollora Code Review\n\n")
    sb.WriteString("## Summary\n\n")
    totalissues :=  result.Summary.CriticalCount + result.Summary.ErrorCount + 
    result.Summary.WarningCount + result.Summary.InfoCount
    sb.WriteString("| Metric | Value |\n")
    sb.WriteString("|--------|-------|\n")
    sb.WriteString(fmt.Sprintf("| Total Issues | %d |\n", totalissues))
    sb.WriteString(fmt.Sprintf("| Critical | %d |\n", result.Summary.CriticalCount))
    sb.WriteString(fmt.Sprintf("| Errors | %d |\n", result.Summary.ErrorCount))
    sb.WriteString(fmt.Sprintf("| Warnings | %d |\n", result.Summary.WarningCount))
    sb.WriteString(fmt.Sprintf("| Infos | %d |\n", result.Summary.InfoCount))
   // sb.WriteString(fmt.Sprintf("| Files Analyzed | %d |\n", result.Summary.FileCount))
    
    // if len(result.OutputFiles) > 0 {
    //     sb.WriteString("\n## Detailed Reports\n\n")
        
    //     for _, file := range result.OutputFiles {
    //         sb.WriteString(fmt.Sprintf("- [%s Report](%s)\n", strings.Title(file.Format), file.URL))
    //     }
    // }
    
    // if result.Summary.TotalIssues > 0 {
    //     sb.WriteString("\n## Issue Breakdown\n\n")
        
    //     sb.WriteString("### By Type\n\n")
    //     sb.WriteString("| Type | Count |\n")
    //     sb.WriteString("|------|-------|\n")
    //     for typ, count := range result.Summary.IssuesByType {
    //         sb.WriteString(fmt.Sprintf("| %s | %d |\n", rh.formatIssueType(typ), count))
    //     }
    
    //     if len(result.Summary.IssuesByFile) > 0 {
    //         sb.WriteString("\n### By File (Top 5)\n\n")
    //         sb.WriteString("| File | Count |\n")
    //         sb.WriteString("|------|-------|\n")
            
    //         type fileCount struct {
    //             file  string
    //             count int
    //         }
    //         var files []fileCount
    //         for file, count := range result.Summary.IssuesByFile {
    //             files = append(files, fileCount{file, count})
    //         }
            
    //         sort.Slice(files, func(i, j int) bool {
    //             return files[i].count > files[j].count
    //         })
            
    //         // Show top 5
    //         limit := 5
    //         if len(files) < limit {
    //             limit = len(files)
    //         }
            
    //         for i := 0; i < limit; i++ {
    //             sb.WriteString(fmt.Sprintf("| `%s` | %d |\n", files[i].file, files[i].count))
    //         }
    //     }
    // }
    
    sb.WriteString("\n\n---\n")
    sb.WriteString("This review was generated automatically by [Gollora](https://github.com/euclidstellar/gollora) :sparkles:")
    
    return sb.String()
}

func (rh *ResponseHandler) parseGitHubPRDiff(ctx context.Context, owner, repo string, prNumber int) (map[string]map[int]bool, error) {
    diffURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNumber)
    
    req, err := http.NewRequestWithContext(ctx, "GET", diffURL, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create diff request: %v", err)
    }
    
    req.Header.Set("Authorization", "token "+rh.config.GitHub.APIToken)
    req.Header.Set("Accept", "application/vnd.github.v3.diff")
    
    client := &http.Client{Timeout: 10 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to get PR diff: %v", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("GitHub API returned error when getting diff: %s", resp.Status)
    }
    
    diffBytes, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil, fmt.Errorf("failed to read diff response: %v", err)
    }
    
    // Parse the diff to identify valid file paths and line numbers
    validPaths := make(map[string]map[int]bool)
    
    diffLines := strings.Split(string(diffBytes), "\n")
    var currentFile string
    var lineOffset int
    
    for _, line := range diffLines {
        if strings.HasPrefix(line, "diff --git") {
            // Extract file path from diff header
            parts := strings.Split(line, " ")
            if len(parts) >= 4 {
                currentFile = strings.TrimPrefix(parts[3], "b/")
                validPaths[currentFile] = make(map[int]bool)
            }
        } else if strings.HasPrefix(line, "@@") {
            // Parse hunk header to get line numbers
            // Format: @@ -oldStart,oldLines +newStart,newLines @@
            parts := strings.Split(line, " ")
            if len(parts) >= 3 {
                newLinePart := strings.TrimPrefix(parts[2], "+")
                newLineParts := strings.Split(newLinePart, ",")
                if len(newLineParts) >= 1 {
                    lineOffset, _ = strconv.Atoi(newLineParts[0])
                    lineOffset--
                }
            }
        } else if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
            lineOffset++
            if currentFile != "" && validPaths[currentFile] != nil {
                validPaths[currentFile][lineOffset] = true
            }
        } else if !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
            lineOffset++
        }
    }
    
    return validPaths, nil
}

func (rh *ResponseHandler) formatIssueComment(issue models.CodeIssue) string {
    var sb strings.Builder
    
    emoji := rh.getEmojiForSeverity(issue.Severity)
    
    sb.WriteString(fmt.Sprintf("%s **%s: %s**\n\n", emoji, issue.Severity, issue.Title))
    sb.WriteString(fmt.Sprintf("%s\n\n", issue.Description))
    
    if issue.Fix != "" {
        sb.WriteString("**Suggested Fix:**\n\n")
        sb.WriteString("```\n")
        sb.WriteString(issue.Fix)
        sb.WriteString("\n```\n\n")
    }
    
    sb.WriteString(fmt.Sprintf("*Detected by %s*", issue.Tool))
    
    if issue.URL != "" {
        sb.WriteString(fmt.Sprintf(" • [More info](%s)", issue.URL))
    }
    
    return sb.String()
}

func (rh *ResponseHandler) getEmojiForSeverity(severity models.IssueSeverity) string {
    switch severity {
    case models.Critical:
        return ":rotating_light:"
    case models.Error:
        return ":x:"
    case models.Warning:
        return ":warning:"
    case models.Info:
        return ":information_source:"
    case models.Hint:
        return ":bulb:"
    default:
        return ":mag:"
    }
}

func (rh *ResponseHandler) formatIssueType(typ string) string {
    switch typ {
    case string(models.Security):
        return "Security"
    case string(models.Performance):
        return "Performance"
    case string(models.Bug):
        return "Bug"
    case string(models.Maintainability):
        return "Maintainability"
    case string(models.Dependency):
        return "Dependency"
    case string(models.CodeStyle):
        return "Code Style"
    case string(models.Test):
        return "Test"
    case string(models.Documentation):
        return "Documentation"
    case string(models.AIInsight):
        return "AI Insight"
    default:
        return typ
    }
}