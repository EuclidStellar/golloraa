package models

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type IssueSeverity string

const (
    Critical IssueSeverity = "CRITICAL"
    Error    IssueSeverity = "ERROR"
    Warning  IssueSeverity = "WARNING"
    Info     IssueSeverity = "INFO"
    Hint     IssueSeverity = "HINT"
)

type IssueType string

const (
    CodeStyle      IssueType = "CODE_STYLE"
    Security       IssueType = "SECURITY"
    Performance    IssueType = "PERFORMANCE"
    Bug            IssueType = "BUG"
    Maintainability IssueType = "MAINTAINABILITY"
    Dependency     IssueType = "DEPENDENCY"
    Test           IssueType = "TEST"
    Documentation  IssueType = "DOCUMENTATION"
    AIInsight      IssueType = "AI_INSIGHT"
)

type CodeIssue struct {
    Title       string       `json:"title"`
    Description string       `json:"description"`
    File        string       `json:"file"`
    Line        int          `json:"line"`
    Column      int          `json:"column,omitempty"`
    Severity    IssueSeverity `json:"severity"`
    Type        IssueType    `json:"type"`
    Tool        string       `json:"tool"`
    Code        string       `json:"code,omitempty"`
    Fix         string       `json:"fix,omitempty"`
    RuleID      string       `json:"rule_id,omitempty"`
    URL         string       `json:"url,omitempty"`
    Message     string       `json:"message,omitempty"`
    Metadata    map[string]string `json:"metadata,omitempty"`
    Source     string       `json:"source,omitempty"`
    Rule       string       `json:"rule,omitempty"`
    Confidence string      `json:"confidence,omitempty"`
    Language   string      `json:"language,omitempty"`
    Event     WebhookEvent `json:"event,omitempty"`
}

type AnalysisResult struct {
    ID           string      `json:"id"`
    Summary      Summary     `json:"summary"`
    Issues       []CodeIssue `json:"issues"`
    Event        WebhookEvent `json:"event"`
    AnalyzedAt   time.Time   `json:"analyzed_at"`
    Duration     float64     `json:"duration_seconds"`
    CompletedAt  time.Time   `json:"completed_at"`
    OutputFiles  []OutputFile `json:"output_files,omitempty"`
    mutex        sync.Mutex
}

type Summary struct {
    TotalIssues      int            `json:"total_issues"`
    CriticalCount    int            `json:"critical_count"`
    ErrorCount       int            `json:"error_count"`
    WarningCount     int            `json:"warning_count"`
    InfoCount        int            `json:"info_count"`
    HintCount        int            `json:"hint_count"`
    FileCount        int            `json:"file_count"`
    IssuesByType     map[string]int `json:"issues_by_type"`
    IssuesByFile     map[string]int `json:"issues_by_file"`
    IssuesByLanguage map[string]int `json:"issues_by_language"`
    IssuesByTool     map[string]int `json:"issues_by_tool"`
    DependencyGraph  string         `json:"dependency_graph,omitempty"`
}

type OutputFile struct {
    Format string `json:"format"`
    Path   string `json:"path"`
    URL    string `json:"url,omitempty"`
}

func NewAnalysisResult(event WebhookEvent) *AnalysisResult {
    return &AnalysisResult{
        ID:         GenerateID(),
        Event:      event,
        AnalyzedAt: time.Now(),
        Summary: Summary{
            IssuesByType:     make(map[string]int),
            IssuesByFile:     make(map[string]int),
            IssuesByLanguage: make(map[string]int),
            IssuesByTool:     make(map[string]int),
        },
        Issues: []CodeIssue{},
    }
}

func (r *AnalysisResult) AddIssue(issue CodeIssue) {
    if len(issue.File) > 0 {
        if issue.Language == "" {
            issue.Language = DetectLanguageFromFile(issue.File)
        }
    }
    
    r.mutex.Lock()
    defer r.mutex.Unlock()

    r.Issues = append(r.Issues, issue)

    switch issue.Severity {
    case Critical:
        r.Summary.CriticalCount++
    case Error:
        r.Summary.ErrorCount++
    case Warning:
        r.Summary.WarningCount++
    case Info:
        r.Summary.InfoCount++
    case Hint:
        r.Summary.HintCount++
    }

    r.Summary.IssuesByType[string(issue.Type)]++
    r.Summary.IssuesByFile[issue.File]++

    r.Summary.IssuesByTool[issue.Tool]++
    language := DetectLanguageFromFile(issue.File)
    r.Summary.IssuesByLanguage[language]++
}

func (r *AnalysisResult) CompleteAnalysis() {
    r.CompletedAt = time.Now()
    r.Duration = r.CompletedAt.Sub(r.AnalyzedAt).Seconds()
    r.Summary.FileCount = len(r.Summary.IssuesByFile)
}

func DetectLanguageFromFile(filePath string) string {
    if filePath == "" {
        return "unknown"
    }

    ext := strings.ToLower(filepath.Ext(filePath))

    if ext == "" {
        fileName := filepath.Base(filePath)
        switch fileName {
        case "Dockerfile":
            return "dockerfile"
        case "Makefile":
            return "makefile"
        case "Gemfile", "Rakefile":
            return "ruby"
        case "package.json", "tsconfig.json":
            return "json"
        case "README.md", "CHANGELOG.md":
            return "markdown"
        default:
            return "unknown"
        }
    }
    ext = strings.TrimPrefix(ext, ".")

    switch ext {
    case "go":
        return "go"
    case "js":
        return "javascript"
    case "ts":
        return "typescript"
    case "jsx":
        return "javascript"
    case "tsx":
        return "typescript"
    case "py":
        return "python"
    case "java":
        return "java"
    case "rb":
        return "ruby"
    case "php":
        return "php"
    case "c", "h":
        return "c"
    case "cpp", "hpp", "cc":
        return "cpp"
    case "cs":
        return "csharp"
    case "rs":
        return "rust"
    case "swift":
        return "swift"
    case "kt", "kts":
        return "kotlin"
    case "sh":
        return "shell"
    case "yaml", "yml":
        return "yaml"
    case "json":
        return "json"
    case "md":
        return "markdown"
    case "html", "htm":
        return "html"
    case "css":
        return "css"
    case "sql":
        return "sql"
    case "dart":
        return "dart"
    default:
        return "unknown"
    }
}

func GenerateID() string {
    return time.Now().Format("20060102-150405-") + RandomString(6)
}

func RandomString(n int) string {
    const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
    result := make([]byte, n)
    for i := range result {
        result[i] = letters[i%len(letters)]
    }
    return string(result)
}