package models

import "time"

// WebhookEvent represents a generic webhook event from a VCS
type WebhookEvent struct {
    Type           string            `json:"type"`
    Provider       string            `json:"provider"` // github, gitlab, bitbucket
    RepoFullName   string            `json:"repo_full_name"`
    RepoURL        string            `json:"repo_url"`
    BaseCommit     string            `json:"base_commit"`
    HeadCommit     string            `json:"head_commit"`
    PullRequestID  int               `json:"pull_request_id,omitempty"`
    PullRequestURL string            `json:"pull_request_url,omitempty"`
    ChangedFiles   []string          `json:"changed_files,omitempty"`
    Metadata       map[string]string `json:"metadata,omitempty"`
    Branch         string            `json:"branch"`
}

// AnalysisRequest represents a request to analyze code
type AnalysisRequest struct {
    Event       WebhookEvent     `json:"event"`
    RepoPath    string           `json:"repo_path"`
    Files       []FileToAnalyze  `json:"files"`
    Settings    AnalysisSettings `json:"settings"`
    RequestedAt time.Time        `json:"requested_at"`
}

// FileToAnalyze contains information about a file to analyze
type FileToAnalyze struct {
    Path          string `json:"path"`
    Language      string `json:"language"`
    Content       string `json:"content,omitempty"`
    BaseCommit    string `json:"base_commit,omitempty"`
    HeadCommit    string `json:"head_commit,omitempty"`
    LinesAdded    int    `json:"lines_added,omitempty"`
    LinesRemoved  int    `json:"lines_removed,omitempty"`
    LinesModified int    `json:"lines_modified,omitempty"`
}

// AnalysisSettings contains settings for code analysis
type AnalysisSettings struct {
    AnalyzeAll        bool     `json:"analyze_all"`
    EnabledLanguages  []string `json:"enabled_languages"`
    EnabledTools      []string `json:"enabled_tools"`
    EnableAI          bool     `json:"enable_ai"`
    ExportFormats     []string `json:"export_formats"`
    CommentThreshold  string   `json:"comment_threshold"` // none, critical, error, warning, info
    IncludeDependency bool     `json:"include_dependency"`
}

// Tool represents a code analysis tool
type Tool struct {
    Name    string   `json:"name"`
    Command string   `json:"command"`
    Args    []string `json:"args"`
    Enabled bool     `json:"enabled"`
}

// LanguageConfig represents the configuration for a programming language
type LanguageConfig struct {
    Enabled bool   `json:"enabled"`
    Tools   []Tool `json:"tools"`
}

// Config represents the application configuration
type Config struct {
    Server struct {
        Port int    `yaml:"port"`
        Host string `yaml:"host"`
    } `yaml:"server"`
    
    GitHub struct {
        WebhookSecret string `yaml:"webhook_secret"`
        APIToken      string `yaml:"api_token"`
    } `yaml:"github"`
    
    GitLab struct {
        WebhookSecret string `yaml:"webhook_secret"`
        APIToken      string `yaml:"api_token"`
    } `yaml:"gitlab"`
    
    Bitbucket struct {
        WebhookSecret string `yaml:"webhook_secret"`
        APIToken      string `yaml:"api_token"`
    } `yaml:"bitbucket"`
    
    Analysis struct {
        Timeout           int `yaml:"timeout"`
        MaxFileSize       int `yaml:"max_file_size"`
        MaxFilesPerReview int `yaml:"max_files_per_review"`
    } `yaml:"analysis"`
    
    AI struct {
        Enabled  bool   `yaml:"enabled"`
        Provider string `yaml:"provider"`
        APIKey   string `yaml:"api_key"`
        Model    string `yaml:"model"`
    } `yaml:"ai"`
    
    Export struct {
        Formats []string `yaml:"formats"`
    } `yaml:"export"`
}

type AnalysisToolsConfig struct {
    Languages map[string]LanguageConfig `yaml:"languages"`
    
}