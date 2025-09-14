package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/euclidstellar/gollora/internal/ai"
	"github.com/euclidstellar/gollora/internal/utils"
	"github.com/euclidstellar/gollora/internal/models"
)

type ResultAggregator struct {
    config *models.Config
}

func NewResultAggregator(config *models.Config) *ResultAggregator {
    return &ResultAggregator{
        config: config,
    }
}

func (ra *ResultAggregator) AggregateResults(result *models.AnalysisResult) *models.AnalysisResult {
    utils.LogWithLocation(utils.Info, "Aggregating results")

    // AI-powered severity scoring if enabled
    if ra.config.AI.Enabled {
        ra.scoreSeverityWithAI(result)
    }

    result = ra.removeDuplicates(result)
    ra.prioritizeIssues(result)

    return result
}

func (ra *ResultAggregator) scoreSeverityWithAI(result *models.AnalysisResult) {
    aiClient := ai.NewClient(ra.config)
    ctx := context.Background()

    for i := range result.Issues {
        issue := &result.Issues[i]
        // Don't re-score issues that already came from the AI
        if issue.Tool == "Gemini" {
            continue
        }

        prompt := fmt.Sprintf(`You are a code quality expert. Analyze the following issue and score its severity.
Code Issue: "%s"
File: %s
Line: %d
Code Snippet:
---
%s
---

Based on the context, rate the severity as one of: "CRITICAL", "ERROR", "WARNING", "INFO".
A hardcoded secret is CRITICAL. A syntax error is an ERROR. A stylistic issue is INFO.
Respond with a JSON object containing "new_severity" and "reason".
Example: {"new_severity": "WARNING", "reason": "This could lead to a null pointer exception if the variable is not initialized."}

Your response:`, issue.Description, issue.File, issue.Line, issue.Code)

        response, err := aiClient.GenerateContent(ctx, prompt)
        if err != nil {
            utils.LogWithLocation(utils.Warn, "AI severity scoring failed for issue: %v", err)
            continue
        }

        var scoringResult struct {
            NewSeverity string `json:"new_severity"`
            Reason      string `json:"reason"`
        }

        response = strings.TrimPrefix(response, "```json")
        response = strings.TrimSuffix(response, "```")
        if err := json.Unmarshal([]byte(response), &scoringResult); err == nil {
            utils.LogWithLocation(utils.Info, "AI re-scored issue severity from %s to %s. Reason: %s", issue.Severity, scoringResult.NewSeverity, scoringResult.Reason)
            issue.Severity = models.IssueSeverity(scoringResult.NewSeverity)
            // Optionally, add the reason to the description
            issue.Description = fmt.Sprintf("%s\n\n**AI Justification**: %s", issue.Description, scoringResult.Reason)
        }
    }
}

func (ra *ResultAggregator) removeDuplicates(result *models.AnalysisResult) *models.AnalysisResult {
    issueMap := make(map[string]models.CodeIssue)
    
    for _, issue := range result.Issues {
        key := issue.File + ":" + string(issue.Line) + ":" + issue.Description
        
        if existing, ok := issueMap[key]; ok {
            if ra.getSeverityWeight(issue.Severity) > ra.getSeverityWeight(existing.Severity) {
                issueMap[key] = issue
            }
        } else {
            issueMap[key] = issue
        }
    }

    dedupedResult := models.NewAnalysisResult(result.Event)
    dedupedResult.AnalyzedAt = result.AnalyzedAt
    dedupedResult.CompletedAt = result.CompletedAt
    dedupedResult.Duration = result.Duration
    dedupedResult.OutputFiles = result.OutputFiles

    for _, issue := range issueMap {
        dedupedResult.AddIssue(issue)
    }
    
    utils.LogWithLocation(utils.Info, "Removed %d duplicate issues", len(result.Issues)-len(dedupedResult.Issues))
    
    return dedupedResult
}

func (ra *ResultAggregator) prioritizeIssues(result *models.AnalysisResult) {

    sort.Slice(result.Issues, func(i, j int) bool {
        sevI := ra.getSeverityWeight(result.Issues[i].Severity)
        sevJ := ra.getSeverityWeight(result.Issues[j].Severity)
        
        if sevI != sevJ {
            return sevI > sevJ
        }
        
        typeI := ra.getTypeWeight(result.Issues[i].Type)
        typeJ := ra.getTypeWeight(result.Issues[j].Type)
        
        if typeI != typeJ {
            return typeI > typeJ
        }
        
        if result.Issues[i].File != result.Issues[j].File {
            return result.Issues[i].File < result.Issues[j].File
        }
    
        return result.Issues[i].Line < result.Issues[j].Line
    })
}


func (ra *ResultAggregator) getSeverityWeight(severity models.IssueSeverity) int {
    switch severity {
    case models.Critical:
        return 4
    case models.Error:
        return 3
    case models.Warning:
        return 2
    case models.Info:
        return 1
    case models.Hint:
        return 0
    default:
        return -1
    }
}

func (ra *ResultAggregator) getTypeWeight(issueType models.IssueType) int {
    switch issueType {
    case models.Security:
        return 5
    case models.Bug:
        return 4
    case models.Performance:
        return 3
    case models.Maintainability:
        return 2
    case models.Dependency:
        return 1
    case models.CodeStyle:
        return 0
    default:
        return -1
    }
}


func (ra *ResultAggregator) FilterIssuesByThreshold(result *models.AnalysisResult, threshold string) []models.CodeIssue {
    var filteredIssues []models.CodeIssue
    
    for _, issue := range result.Issues {
        severityValue := severityToValue(string(issue.Severity))
        thresholdValue := severityToValue(threshold)
        
        if severityValue >= thresholdValue {
            filteredIssues = append(filteredIssues, issue)
        }
    }
    
    return filteredIssues
}

func severityToValue(severity string) int {
    switch strings.ToLower(severity) {
    case "critical":
        return 4
    case "high":
        return 3
    case "medium":
        return 2
    case "low":
        return 1
    case "info":
        return 0
    default:
        return 0
    }
}