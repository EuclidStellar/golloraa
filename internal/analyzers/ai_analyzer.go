package analyzers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/euclidstellar/gollora/internal/models"
	"github.com/euclidstellar/gollora/internal/utils"
)

type AIAnalyzer struct {
    provider string
    apiKey   string
    model    string
}

func NewAIAnalyzer(provider, apiKey, model string) *AIAnalyzer {
    return &AIAnalyzer{
        provider: provider,
        apiKey:   apiKey,
        model:    model,
    }
}

func (a *AIAnalyzer) Analyze(ctx context.Context, files []models.FileToAnalyze) ([]models.CodeIssue, error) {
    var allIssues []models.CodeIssue

    if a.apiKey == "" {
        return nil, fmt.Errorf("AI API key not provided")
    }

    utils.LogWithLocation(utils.Info, "Running AI analysis on %d files", len(files))

    for _, file := range files {
        if file.Content == "" {
            continue
        }

        utils.LogWithLocation(utils.Info, "Analyzing %s with AI", file.Path)

        switch a.provider {
        case "gemini":
            issues , err := a.analyzeWithGemini(ctx, file)
            if err != nil {
                utils.LogWithLocation(utils.Error, "Error analyzing %s with Gemini: %v", file.Path, err)
                continue
            }
            allIssues = append(allIssues, issues...)
        default:
            utils.LogWithLocation(utils.Error, "Unsupported AI provider: %s", a.provider)
            return nil, fmt.Errorf("unsupported AI provider: %s", a.provider)
        }
    }

    return allIssues, nil
}

func recoverPartialJSON(input string) string {

    if strings.HasPrefix(input, "[") && !strings.HasSuffix(input, "]") {
        var objects []string
        openBrace := 0
        openBracket := 0
        start := -1
        
        for i, c := range input {
            if c == '[' {
                openBracket++
                if start == -1 && openBracket == 1 {
                    start = i
                }
            } else if c == ']' {
                openBracket--
            } else if c == '{' {
                if openBrace == 0 {
                    start = i
                }
                openBrace++
            } else if c == '}' {
                openBrace--
                if openBrace == 0 && start != -1 {
                    objects = append(objects, input[start:i+1])
                    start = -1
                }
            }
        }
        
        if len(objects) > 0 {
            return "[" + strings.Join(objects, ",") + "]"
        }
    }
    
    return input
}

func (a *AIAnalyzer) analyzeWithGemini(ctx context.Context, file models.FileToAnalyze) ([]models.CodeIssue, error) {
    endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", a.model)
    prompt := fmt.Sprintf(`Analyze the following code and identify issues related to code quality, security, performance, or best practices.
File: %s
Language: %s

%s

YOU MUST RETURN ONLY THE JSON ARRAY WITHOUT ANY MARKDOWN FORMATTING.
Do not include backticks, "json", or any other text.
Focus on the most critical issues only, and limit your response to at most 5 issues.
Return only the raw JSON array using this exact structure:
[
  {
    "title": "Brief issue title",
    "description": "Detailed description of the issue",
    "line": line_number,
    "severity": "CRITICAL|ERROR|WARNING|INFO",
    "type": "SECURITY|BUG|PERFORMANCE|MAINTAINABILITY|CODE_STYLE",
    "fix": "Suggested code fix (optional)"
  }
]

If you find no issues, return an empty array: []
`, 
file.Path, file.Language, file.Content)

    requestBody := map[string]interface{}{
        "contents": []map[string]interface{}{
            {
                "parts": []map[string]interface{}{
                    {
                        "text": prompt,
                    },
                },
            },
        },
        "generationConfig": map[string]interface{}{
            "temperature": 0.1,
            "maxOutputTokens": 1024,
            "topP": 0.95,
            "topK": 40,
        },
    }

    requestJSON, err := json.Marshal(requestBody)
    if err != nil {
        return nil, err
    }

    requestURL := fmt.Sprintf("%s?key=%s", endpoint, a.apiKey)
    req, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewBuffer(requestJSON))
    if err != nil {
        return nil, err
    }

    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: 30 * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("gemini API returned status: %s, body: %s", resp.Status, string(body))
    }

    var result struct {
        Candidates []struct {
            Content struct {
                Parts []struct {
                    Text string `json:"text"`
                } `json:"parts"`
            } `json:"content"`
        } `json:"candidates"`
    }

    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }

    if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
        return nil, nil
    }

    content := result.Candidates[0].Content.Parts[0].Text
    utils.LogWithLocation(utils.Debug, "Gemini raw response: %s", content)

    content = strings.ReplaceAll(content, "```json", "")
    content = strings.ReplaceAll(content, "```", "")

    jsonContent := content
    if strings.Contains(content, "[") && strings.Contains(content, "]") {
        start := strings.Index(content, "[")
        end := strings.LastIndex(content, "]") + 1
        if start < end {
            jsonContent = content[start:end]
        }
    }

    jsonContent = strings.TrimSpace(jsonContent)
    utils.LogWithLocation(utils.Debug, "Extracted JSON content: %s", jsonContent)

    if len(jsonContent) < 5 {
        utils.LogWithLocation(utils.Warn, "Gemini returned empty or too short JSON response")
        return []models.CodeIssue{}, nil
    }

    if !strings.HasPrefix(jsonContent, "[") || !strings.HasSuffix(jsonContent, "]") {
        utils.LogWithLocation(utils.Warn, "Gemini response is not a valid JSON array: %s", jsonContent)

        if strings.Contains(jsonContent, "[{") && strings.Contains(jsonContent, "}]") {
            start := strings.Index(jsonContent, "[{")
            end := strings.LastIndex(jsonContent, "}]") + 2
            if start < end {
                jsonContent = jsonContent[start:end]
                utils.LogWithLocation(utils.Info, "Found JSON array within content: %s", jsonContent)
            }
        }

        if !strings.HasPrefix(jsonContent, "[") || !strings.HasSuffix(jsonContent, "]") {
            if strings.Contains(jsonContent, "{") && strings.Contains(jsonContent, "}") {
                var objects []string
                openBrace := 0
                start := -1
                for i, c := range jsonContent {
                    if c == '{' {
                        if openBrace == 0 {
                            start = i
                        }
                        openBrace++
                    } else if c == '}' {
                        openBrace--
                        if openBrace == 0 && start != -1 {
                            objects = append(objects, jsonContent[start:i+1])
                            start = -1
                        }
                    }
                }
                
                if len(objects) > 0 {
                    jsonContent = "[" + strings.Join(objects, ",") + "]"
                } else {
                    return []models.CodeIssue{}, nil
                }
            } else {
                return []models.CodeIssue{}, nil
            }
        }
    }

    var aiIssues []struct {
        Title       string `json:"title"`
        Description string `json:"description"`
        Line        int    `json:"line"`
        Severity    string `json:"severity"`
        Type        string `json:"type"`
        Fix         string `json:"fix,omitempty"`
    }

    if err := json.Unmarshal([]byte(jsonContent), &aiIssues); err != nil {
        recoveredJSON := recoverPartialJSON(jsonContent)
        if recoveredJSON != jsonContent {
            utils.LogWithLocation(utils.Info, "Attempting to recover from partial JSON")
            if err := json.Unmarshal([]byte(recoveredJSON), &aiIssues); err != nil {
                return nil, fmt.Errorf("failed to parse AI response even after recovery: %v, content: %s", err, jsonContent)
            }
            utils.LogWithLocation(utils.Info, "Successfully recovered %d issues from partial JSON", len(aiIssues))
        } else {
            return nil, fmt.Errorf("failed to parse AI response: %v, content: %s", err, jsonContent)
        }
    }

    var issues []models.CodeIssue
    for _, aiIssue := range aiIssues {
        var severity models.IssueSeverity
        switch strings.ToUpper(aiIssue.Severity) {
        case "CRITICAL":
            severity = models.Critical
        case "ERROR":
            severity = models.Error
        case "WARNING":
            severity = models.Warning
        case "INFO":
            severity = models.Info
        default:
            severity = models.Info
        }

        var issueType models.IssueType
        switch strings.ToUpper(aiIssue.Type) {
        case "SECURITY":
            issueType = models.Security
        case "BUG":
            issueType = models.Bug
        case "PERFORMANCE":
            issueType = models.Performance
        case "MAINTAINABILITY":
            issueType = models.Maintainability
        case "CODE_STYLE":
            issueType = models.CodeStyle
        default:
            issueType = models.AIInsight
        }

        issues = append(issues, models.CodeIssue{
            Title:       aiIssue.Title,
            Description: aiIssue.Description,
            File:        file.Path,
            Line:        aiIssue.Line,
            Severity:    severity,
            Type:        issueType,
            Tool:        "Gemini",
            Fix:         aiIssue.Fix,
        })
    }

    return issues, nil
}





// dump code with token limit exceeded in gemini 


// func (a *AIAnalyzer) analyzeWithGemini(ctx context.Context, file models.FileToAnalyze) ([]models.CodeIssue, error) {
//    // Update this line:
//     endpoint := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", a.model)
//     prompt := fmt.Sprintf(`Analyze the following code and identify issues related to code quality, security, performance, or best practices
// File: %s
// Language: %s

// %s

// YOU MUST RETURN VALID JSON ONLY. Do not include any explanatory text, code, or comments outside the JSON array.
// Use this exact JSON structure for your response:
// [
//   {
//     "title": "Brief issue title",
//     "description": "Detailed description of the issue",
//     "line": line_number,
//     "severity": "CRITICAL|ERROR|WARNING|INFO",
//     "type": "SECURITY|BUG|PERFORMANCE|MAINTAINABILITY|CODE_STYLE",
//     "fix": "Suggested code fix (optional)"
//   }
// ]
// `, file.Path, file.Language, file.Content)

//     requestBody := map[string]interface{}{
//         "contents": []map[string]interface{}{
//             {
//                 "parts": []map[string]interface{}{
//                     {
//                         "text": prompt,
//                     },
//                 },
//             },
//         },
//         "generationConfig": map[string]interface{}{
//             "temperature": 0.2,
//             "maxOutputTokens": 1024,
//             "topP": 0.8,
//             "topK": 40,
//         },
//     }

//     requestJSON, err := json.Marshal(requestBody)
//     if err != nil {
//         return nil, err
//     }

//     // Construct the URL with API key
//     requestURL := fmt.Sprintf("%s?key=%s", endpoint, a.apiKey)
//     req, err := http.NewRequestWithContext(ctx, "POST", requestURL, bytes.NewBuffer(requestJSON))
//     if err != nil {
//         return nil, err
//     }

//     req.Header.Set("Content-Type", "application/json")

//     client := &http.Client{Timeout: 30 * time.Second}
//     resp, err := client.Do(req)
//     if err != nil {
//         return nil, err
//     }
//     defer resp.Body.Close()

//     if resp.StatusCode != http.StatusOK {
//         body, _ := io.ReadAll(resp.Body)
//         return nil, fmt.Errorf("gemini API returned status: %s, body: %s", resp.Status, string(body))
//     }

//     var result struct {
//         Candidates []struct {
//             Content struct {
//                 Parts []struct {
//                     Text string `json:"text"`
//                 } `json:"parts"`
//             } `json:"content"`
//         } `json:"candidates"`
//     }

//     if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
//         return nil, err
//     }

//     if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
//         return nil, nil
//     }

//     // Parse the JSON response from the AI model
//     content := result.Candidates[0].Content.Parts[0].Text
//     utils.LogWithLocation(utils.Debug, "Gemini raw response: %s", content)
//     // Extract the JSON part if it's wrapped in text
//     if strings.Contains(content, "```json") {
//         content = strings.ReplaceAll(content, "```json", "")
//         content = strings.ReplaceAll(content, "```", "")
//     }
//     jsonContent := content
//     if strings.Contains(content, "[") && strings.Contains(content, "]") {
//         start := strings.Index(content, "[")
//         end := strings.LastIndex(content, "]") + 1
//         if start < end {
//             jsonContent = content[start:end]
//         }
//     }
    
//     jsonContent = strings.TrimSpace(jsonContent)

//     if len(jsonContent) < 5 {
//         utils.LogWithLocation(utils.Warn, "Gemini returned empty or too short JSON response")
//         return []models.CodeIssue{}, nil
//     }

//     if !strings.HasPrefix(jsonContent, "[") || !strings.HasSuffix(jsonContent, "]") {
//         utils.LogWithLocation(utils.Warn, "Gemini response is not a valid JSON array: %s", jsonContent)
//         // Try to create a valid JSON array with partial data
//         if strings.Contains(jsonContent, "{") && strings.Contains(jsonContent, "}") {
//             // Extract JSON objects and construct a valid array
//             var objects []string
//             openBrace := 0
//             start := -1
//             for i, c := range jsonContent {
//                 if c == '{' {
//                     if openBrace == 0 {
//                         start = i
//                     }
//                     openBrace++
//                 } else if c == '}' {
//                     openBrace--
//                     if openBrace == 0 && start != -1 {
//                         objects = append(objects, jsonContent[start:i+1])
//                         start = -1
//                     }
//                 }
//             }
            
//             if len(objects) > 0 {
//                 jsonContent = "[" + strings.Join(objects, ",") + "]"
//             } else {
//                 // No valid objects found, return empty results
//                 return []models.CodeIssue{}, nil
//             }
//         } else {
//             // Not a JSON object format either, return empty results
//             return []models.CodeIssue{}, nil
//         }
//     }

//     var aiIssues []struct {
//         Title       string `json:"title"`
//         Description string `json:"description"`
//         Line        int    `json:"line"`
//         Severity    string `json:"severity"`
//         Type        string `json:"type"`
//         Fix         string `json:"fix,omitempty"`
//     }

//     if err := json.Unmarshal([]byte(content), &aiIssues); err != nil {
//         return nil, fmt.Errorf("failed to parse AI response: %v", err)
//     }

//     var issues []models.CodeIssue
//     for _, aiIssue := range aiIssues {
//         var severity models.IssueSeverity
//         switch strings.ToUpper(aiIssue.Severity) {
//         case "CRITICAL":
//             severity = models.Critical
//         case "ERROR":
//             severity = models.Error
//         case "WARNING":
//             severity = models.Warning
//         case "INFO":
//             severity = models.Info
//         default:
//             severity = models.Info
//         }

//         var issueType models.IssueType
//         switch strings.ToUpper(aiIssue.Type) {
//         case "SECURITY":
//             issueType = models.Security
//         case "BUG":
//             issueType = models.Bug
//         case "PERFORMANCE":
//             issueType = models.Performance
//         case "MAINTAINABILITY":
//             issueType = models.Maintainability
//         case "CODE_STYLE":
//             issueType = models.CodeStyle
//         default:
//             issueType = models.AIInsight
//         }

//         issues = append(issues, models.CodeIssue{
//             Title:       aiIssue.Title,
//             Description: aiIssue.Description,
//             File:        file.Path,
//             Line:        aiIssue.Line,
//             Severity:    severity,
//             Type:        issueType,
//             Tool:        "Gemini",
//             Fix:         aiIssue.Fix,
//         })
//     }

//     return issues, nil
// }

// Updated analyzeWithGemini function
// Add this function to your AI analyzer