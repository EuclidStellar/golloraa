package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/euclidstellar/gollora/internal/agent"
	"github.com/euclidstellar/gollora/internal/models"
	"github.com/euclidstellar/gollora/internal/utils"
	"gopkg.in/yaml.v3"
)

var (
    configPath  = flag.String("config", "configs/config.yaml", "Path to the configuration file")
    toolsPath   = flag.String("tools", "configs/analysis_tools.yaml", "Path to the analysis tools configuration file")
    logDir      = flag.String("log-dir", "logs", "Directory for log files")
    serverMode  = flag.Bool("server", false, "Run in server mode")
    analyzeMode = flag.Bool("analyze", false, "Run in analyze mode")
    qaMode      = flag.Bool("qa", false, "Run in interactive Q&A mode")
    port        = flag.Int("port", 0, "Port to run the server on (overrides config)")
    
    // Analyze mode flags
    repoPath    = flag.String("repo-path", "", "Path to repository to analyze")
    baseCommit  = flag.String("base-commit", "", "Base commit for comparison")
    headCommit  = flag.String("head-commit", "", "Head commit for comparison")
    outputDir   = flag.String("output-dir", "", "Directory for analysis output")
    
    // Event flags
    eventType     = flag.String("event-type", "", "Event type (push, pull_request)")
    repoURL       = flag.String("repo-url", "", "Repository URL")
    prID          = flag.Int("pr-id", 0, "Pull request ID")
    prURL         = flag.String("pr-url", "", "Pull request URL")
)

func main() {
    flag.Parse()
    
    if err := os.MkdirAll(*logDir, 0755); err != nil {
        log.Fatalf("Failed to create log directory: %v", err)
    }
    
    logFile, err := utils.SetupLogFile(*logDir)
    if err != nil {
        log.Fatalf("Failed to set up logging: %v", err)
    }
    defer logFile.Close()
    
    utils.LogWithLocation(utils.Info, "Starting Gollora")
    
    config, toolsConfig, err := loadConfigurations(*configPath, *toolsPath)
    if err != nil {
        utils.LogWithLocation(utils.Error, "Failed to load configurations: %v", err)
        os.Exit(1)
    }
    
    if *port > 0 {
        config.Server.Port = *port
    }

    loadAPIKeysFromEnv(config)

    if *serverMode {
        runServer(config, toolsConfig)
    } else if *analyzeMode {
        runAnalyze(config, toolsConfig)
    } else if *qaMode {
        runQA(config)
    } else if *repoPath != "" {
        runDirectAnalysis(config, toolsConfig)
    } else {
        flag.Usage()
        os.Exit(1)
    }
}

func loadConfigurations(configPath, toolsPath string) (*models.Config, *models.AnalysisToolsConfig, error) {
    configData, err := os.ReadFile(configPath)
    if err != nil {
        return nil, nil, fmt.Errorf("failed to read config file: %v", err)
    }
    
    var config models.Config
    if err := yaml.Unmarshal(configData, &config); err != nil {
        return nil, nil, fmt.Errorf("failed to parse config file: %v", err)
    }
    
    toolsData, err := os.ReadFile(toolsPath)
    if err != nil {
        return nil, nil, fmt.Errorf("failed to read tools config file: %v", err)
    }
    
    var toolsConfig models.AnalysisToolsConfig
    if err := yaml.Unmarshal(toolsData, &toolsConfig); err != nil {
        return nil, nil, fmt.Errorf("failed to parse tools config file: %v", err)
    }
    
    return &config, &toolsConfig, nil
}

func loadAPIKeysFromEnv(config *models.Config) {
    if token := os.Getenv("GITHUB_API_TOKEN"); token != "" {
        config.GitHub.APIToken = token
    }
   
    if secret := os.Getenv("GITHUB_WEBHOOK_SECRET"); secret != "" {
        config.GitHub.WebhookSecret = secret
    }
   
    if token := os.Getenv("GITLAB_API_TOKEN"); token != "" {
        config.GitLab.APIToken = token
    }

    if secret := os.Getenv("GITLAB_WEBHOOK_SECRET"); secret != "" {
        config.GitLab.WebhookSecret = secret
    }

    if apiKey := os.Getenv("AI_API_KEY"); apiKey != "" {
        config.AI.APIKey = apiKey
    }
}

func runServer(config *models.Config, toolsConfig *models.AnalysisToolsConfig) {
    webhookHandler := NewWebhookHandler(config, toolsConfig)
    
    addr := fmt.Sprintf("%s:%d", config.Server.Host, config.Server.Port)
    server := &http.Server{
        Addr:    addr,
        Handler: webhookHandler,
    }
    
    // Start the server in a goroutine
    go func() {
        utils.LogWithLocation(utils.Info, "Starting server on %s", addr)
        if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            utils.LogWithLocation(utils.Error, "Failed to start server: %v", err)
            os.Exit(1)
        }
    }()
    
    stop := make(chan os.Signal, 1)
    signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
    <-stop 
    
    utils.LogWithLocation(utils.Info, "Shutting down server...")
    
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if err := server.Shutdown(ctx); err != nil {
        utils.LogWithLocation(utils.Error, "Server shutdown failed: %v", err)
    }
    
    utils.LogWithLocation(utils.Info, "Server stopped")
}

func runAnalyze(config *models.Config, toolsConfig *models.AnalysisToolsConfig) {
    if *eventType == "" || *repoURL == "" || *baseCommit == "" || *headCommit == "" {
        utils.LogWithLocation(utils.Error, "Missing required arguments for analyze mode")
        flag.Usage()
        os.Exit(1)
    }

    event := models.WebhookEvent{
        Type:       *eventType,
        RepoURL:    *repoURL,
        BaseCommit: *baseCommit,
        HeadCommit: *headCommit,
    }
    
    if *eventType == "pull_request" {
        event.PullRequestID = *prID
        event.PullRequestURL = *prURL
    }
    repoFullName := extractRepoFullNameFromURL(*repoURL)
    event.RepoFullName = repoFullName

    runAnalysisProcess(context.Background(), config, toolsConfig, event)
}

func runDirectAnalysis(config *models.Config, toolsConfig *models.AnalysisToolsConfig) {
    if *repoPath == "" || *baseCommit == "" || *headCommit == "" {
        utils.LogWithLocation(utils.Error, "Missing required arguments for direct analysis")
        flag.Usage()
        os.Exit(1)
    }
    
    event := models.WebhookEvent{
        Type:       "local",
        RepoURL:    *repoPath, 
        BaseCommit: *baseCommit,
        HeadCommit: *headCommit,
    }

    outDir := *outputDir
    if outDir == "" {
        outDir = filepath.Join(*repoPath, "code-review-output")
    }

    ctx := context.Background()
   // fetcher := NewCodeFetcher()

    utils.LogWithLocation(utils.Info, "Getting changed files in local repository")
    changedFiles, err := utils.GetChangedFiles(*repoPath, *baseCommit, *headCommit)
    if err != nil {
        utils.LogWithLocation(utils.Error, "Failed to get changed files: %v", err)
        os.Exit(1)
    }

    var filesToAnalyze []models.FileToAnalyze
    for _, file := range changedFiles {
        if shouldSkipFile(file , os.TempDir()) {
            utils.LogWithLocation(utils.Info, "Skipping file: %s", file)
            continue
        }
        
        content, err := os.ReadFile(filepath.Join(*repoPath, file))
        if err != nil {
            utils.LogWithLocation(utils.Warn, "Failed to read file %s: %v", file, err)
            continue
        }

        language := utils.DetectFileLanguage(file)
        
        filesToAnalyze = append(filesToAnalyze, models.FileToAnalyze{
            Path:       file,
            Language:   language,
            Content:    string(content),
            BaseCommit: *baseCommit,
            HeadCommit: *headCommit,
        })
    }
    
	request := models.AnalysisRequest{
        Event:       event,
        RepoPath:    *repoPath,
        Files:       filesToAnalyze,
        RequestedAt: time.Now(),
        Settings: models.AnalysisSettings{
            AnalyzeAll:        true,
            EnabledLanguages:  []string{},
            EnabledTools:      []string{},
            EnableAI:          config.AI.Enabled,
            ExportFormats:     config.Export.Formats,
            CommentThreshold:  "warning",
            IncludeDependency: true,
        },
    }
    
   // engine = ReviewEngine(config, toolsConfig)

    engine := NewReviewEngine(config, toolsConfig)
    result, err := engine.Analyze(ctx, request)
    if err != nil {
        utils.LogWithLocation(utils.Error, "Analysis failed: %v", err)
        os.Exit(1)
    }

	if err := os.MkdirAll(outDir, 0755); err != nil {
        utils.LogWithLocation(utils.Error, "Failed to create output directory: %v", err)
        os.Exit(1)
    }

    for _, format := range config.Export.Formats {
        switch format {
        case "json":
            jsonPath := filepath.Join(outDir, fmt.Sprintf("code-review-%s.json", result.ID))
            if err := utils.FormatToJSON(result, jsonPath); err != nil {
                utils.LogWithLocation(utils.Error, "Failed to export to JSON: %v", err)
                continue
            }
            utils.LogWithLocation(utils.Info, "JSON report saved to: %s", jsonPath)
            
        case "markdown":
			mdPath := filepath.Join(outDir, fmt.Sprintf("code-review-%s.md", result.ID))
            if _, err := utils.FormatToMarkdown(result, mdPath); err != nil {
                utils.LogWithLocation(utils.Error, "Failed to export to Markdown: %v", err)
                continue
            }
            utils.LogWithLocation(utils.Info, "Markdown report saved to: %s", mdPath)
        }
    }
    
    utils.LogWithLocation(utils.Info, "Analysis complete! Found %d issues", result.Summary.TotalIssues)
}

func runQA(config *models.Config) {
    if *repoPath == "" {
        utils.LogWithLocation(utils.Error, "The -repo-path flag is required for Q&A mode")
        flag.Usage()
        os.Exit(1)
    }

    ctx := context.Background()

    // Define a progress callback for the CLI
    progressCallback := func(message string) {
        fmt.Printf("... %s\n", message)
    }

    agent, err := agent.NewAgent(ctx, config, *repoPath, progressCallback)
    if err != nil {
        utils.LogWithLocation(utils.Error, "Failed to initialize Q&A agent: %v", err)
        os.Exit(1)
    }

    fmt.Println("✅ Agent is ready. Ask questions about your codebase. Type 'exit' to quit.")
    scanner := bufio.NewScanner(os.Stdin)
    for {
        fmt.Print("> ")
        if !scanner.Scan() {
            break
        }
        question := scanner.Text()
        if strings.ToLower(question) == "exit" {
            break
        }

        fmt.Println("🤖 Thinking...")
        answer, err := agent.Ask(ctx, question)
        if err != nil {
            utils.LogWithLocation(utils.Error, "Failed to get answer: %v", err)
            continue
        }
        fmt.Println(answer)
    }
}

func runAnalysisProcess(ctx context.Context, config *models.Config, toolsConfig *models.AnalysisToolsConfig, event models.WebhookEvent) {
    utils.LogWithLocation(utils.Info, "Starting analysis process for event: %s", event.Type)

    fetcher := NewCodeFetcher()
    engine := NewReviewEngine(config, toolsConfig)
    responseHandler := NewResponseHandler(config)

    repoPath, files, err := fetcher.FetchCode(ctx, event)
    if err != nil {
        utils.LogWithLocation(utils.Error, "Failed to fetch code: %v", err)
        return
    }
    defer os.RemoveAll(repoPath)

    request := models.AnalysisRequest{
        Event:       event,
        RepoPath:    repoPath,
        Files:       files,
        RequestedAt: time.Now(),
        Settings: models.AnalysisSettings{
			AnalyzeAll:        true,
            EnabledLanguages:  []string{},
            EnabledTools:      []string{},
            EnableAI:          config.AI.Enabled,
            ExportFormats:     config.Export.Formats,
            CommentThreshold:  "warning",
            IncludeDependency: false,
        },
    }

    result, err := engine.Analyze(ctx, request)
    if err != nil {
        utils.LogWithLocation(utils.Error, "Analysis failed: %v", err)
        return
    }

    if event.Type == "pull_request" && event.PullRequestURL != "" {
        if err := responseHandler.SendResponse(ctx, result); err != nil {
            utils.LogWithLocation(utils.Error, "Failed to send response: %v", err)
            return
        }
    }
    
    utils.LogWithLocation(utils.Info, "Analysis process completed successfully")
}

func extractRepoFullNameFromURL(url string) string {
    for _, prefix := range []string{
        "https://github.com/",
        "git@github.com:",
        "git@gitlab.com:",
		} {
			if strings.HasPrefix(url, prefix) {
				fullPath := strings.TrimPrefix(url, prefix)
				fullPath = strings.TrimSuffix(fullPath, ".git")
				parts := strings.Split(fullPath, "/")
				if len(parts) >= 2 {
					return parts[0] + "/" + parts[1]
				}
			}
		}
		return ""
	}