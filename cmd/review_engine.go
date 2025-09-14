package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/euclidstellar/gollora/internal/analyzers"
	"github.com/euclidstellar/gollora/internal/models"
	"github.com/euclidstellar/gollora/internal/utils"
	"golang.org/x/mod/modfile"
)

type ReviewEngine struct {
    config      *models.Config
    toolsConfig *models.AnalysisToolsConfig
}

func NewReviewEngine(config *models.Config, toolsConfig *models.AnalysisToolsConfig) *ReviewEngine {
    return &ReviewEngine{
        config:      config,
        toolsConfig: toolsConfig,
    }
}

func (re *ReviewEngine) Analyze(ctx context.Context, request models.AnalysisRequest) (*models.AnalysisResult, error) {
    utils.LogWithLocation(utils.Info, "Starting code analysis for %s", request.Event.RepoFullName)
    result := models.NewAnalysisResult(request.Event)

    filesByLang := make(map[string][]models.FileToAnalyze)
    for _, file := range request.Files {
        filesByLang[file.Language] = append(filesByLang[file.Language], file)
    }
    
    var wg sync.WaitGroup
    var resultMutex sync.Mutex // Mutex to protect result from concurrent writes

    for lang, files := range filesByLang {
        if !re.isLanguageEnabled(lang, request.Settings.EnabledLanguages) {
            continue
        }
        
        wg.Add(1)
        go func(language string, languageFiles []models.FileToAnalyze) {
            defer wg.Done()
            
            utils.LogWithLocation(utils.Info, "Analyzing %d %s files", len(languageFiles), language)
            tools, ok := re.toolsConfig.Languages[language]
            if !ok || !tools.Enabled {
                utils.LogWithLocation(utils.Warn, "No tools configured for language: %s", language)
                return
            }
            
            // Create and run the appropriate analyzer
            var issues []models.CodeIssue
            var err error
            
            switch language {
            case "go":
                analyzer := analyzers.NewGolangAnalyzer(tools.Tools)
                issues, err = analyzer.Analyze(ctx, request.RepoPath, languageFiles)
            case "python":
                analyzer := analyzers.NewPythonAnalyzer(tools.Tools)
                issues, err = analyzer.Analyze(ctx, request.RepoPath, languageFiles)
            default:
                utils.LogWithLocation(utils.Info, "No analyzer available for language: %s", language)
                return
            }
            
            if err != nil {
                utils.LogWithLocation(utils.Error, "Error analyzing %s files: %v", language, err)
                return
            }

            resultMutex.Lock()
            for _, issue := range issues {
                result.AddIssue(issue)
            }
            resultMutex.Unlock()
            
        }(lang, files)
    }
    
    if request.Settings.EnableAI && re.config.AI.Enabled && re.config.AI.APIKey != "" {
        wg.Add(1)
        go func() {
            defer wg.Done()
            
            utils.LogWithLocation(utils.Info, "Running AI analysis")
            aiAnalyzer := analyzers.NewAIAnalyzer(
                re.config.AI.Provider,
                re.config.AI.APIKey,
                re.config.AI.Model,
            )
           
            filesToAnalyze := filterFilesForAI(request.Files, 5)
            
            issues, err := aiAnalyzer.Analyze(ctx, filesToAnalyze)
            if err != nil {
                utils.LogWithLocation(utils.Error, "Error running AI analysis: %v", err)
                return
            }

            resultMutex.Lock()
            for _, issue := range issues {
                result.AddIssue(issue)
            }
            resultMutex.Unlock()
        }()
    }

    wg.Wait()

    // Perform dependency analysis
    analyzeDependencies(request.RepoPath, result)

    result.CompleteAnalysis()

    outputDir := filepath.Join(request.RepoPath, "code-review-output")
    if err := os.MkdirAll(outputDir, 0755); err != nil {
        utils.LogWithLocation(utils.Error, "Failed to create output directory: %v", err)
    } else {
        re.exportResults(result, outputDir, request.Settings.ExportFormats)
    }
    
    utils.LogWithLocation(utils.Info, "Analysis complete. Found %d issues (%d critical, %d error, %d warning)",
        result.Summary.TotalIssues, result.Summary.CriticalCount, result.Summary.ErrorCount, result.Summary.WarningCount)
    
    return result, nil
}

func (re *ReviewEngine) isLanguageEnabled(language string, enabledLanguages []string) bool {
    if len(enabledLanguages) == 0 {
        if langConfig, ok := re.toolsConfig.Languages[language]; ok {
            return langConfig.Enabled
        }
        return false
    }

    for _, lang := range enabledLanguages {
        if lang == language {
            return true
        }
    }
    
    return false
}

func filterFilesForAI(files []models.FileToAnalyze, maxFiles int) []models.FileToAnalyze {
    if len(files) <= maxFiles {
        return files
    }
  
    var prioritizedFiles []models.FileToAnalyze
    var otherFiles []models.FileToAnalyze
    
    for _, file := range files {
        if isHighPriorityFile(file.Path) {
            prioritizedFiles = append(prioritizedFiles, file)
        } else {
            otherFiles = append(otherFiles, file)
        }
    }
   
    if len(prioritizedFiles) >= maxFiles {
        return prioritizedFiles[:maxFiles]
    }
    
    remaining := maxFiles - len(prioritizedFiles)
    if remaining > len(otherFiles) {
        remaining = len(otherFiles)
    }
    
    return append(prioritizedFiles, otherFiles[:remaining]...)
}

func isHighPriorityFile(path string) bool {
    ext := filepath.Ext(path)
    

    switch ext {
    case ".go", ".js", ".ts", ".py", ".java", ".c", ".cpp", ".cs", ".php", ".rb":
        return true
    }

    basename := filepath.Base(path)
    switch basename {
    case "Dockerfile", "docker-compose.yml", "docker-compose.yaml",
        ".gitlab-ci.yml", ".github/workflows/ci.yml", "Jenkinsfile",
        "package.json", "go.mod", "requirements.txt", "pom.xml":
        return true
    }
    
    return false
}

// analyzeDependencies parses dependency files and generates a visualization graph.
func analyzeDependencies(repoPath string, result *models.AnalysisResult) {
    utils.LogWithLocation(utils.Info, "Starting dependency analysis.")
    var graphBuilder strings.Builder
    graphBuilder.WriteString("graph TD\n")
    foundDependencies := false

    // --- Go Dependency Analysis ---
    goModPath := filepath.Join(repoPath, "go.mod")
    if _, err := os.Stat(goModPath); err == nil {
        utils.LogWithLocation(utils.Info, "Found go.mod, analyzing Go dependencies.")
        goDeps, err := parseGoMod(goModPath)
        if err != nil {
            utils.LogWithLocation(utils.Warn, "Failed to parse go.mod: %v", err)
        } else {
            projectName := "Project(Go)"
            graphBuilder.WriteString(fmt.Sprintf("    subgraph %s\n", projectName))
            for _, dep := range goDeps {
                graphBuilder.WriteString(fmt.Sprintf("        %s --> \"%s\";\n", projectName, dep))
            }
            graphBuilder.WriteString("    end\n")
            foundDependencies = true
        }
    }

    // --- Python Dependency Analysis ---
    reqPath := filepath.Join(repoPath, "requirements.txt")
    if _, err := os.Stat(reqPath); err == nil {
        utils.LogWithLocation(utils.Info, "Found requirements.txt, analyzing Python dependencies.")
        pyDeps, err := parseRequirementsTxt(reqPath)
        if err != nil {
            utils.LogWithLocation(utils.Warn, "Failed to parse requirements.txt: %v", err)
        } else {
            projectName := "Project(Python)"
            graphBuilder.WriteString(fmt.Sprintf("    subgraph %s\n", projectName))
            for _, dep := range pyDeps {
                graphBuilder.WriteString(fmt.Sprintf("        %s --> \"%s\";\n", projectName, dep))
            }
            graphBuilder.WriteString("    end\n")
            foundDependencies = true
        }
    }

    if foundDependencies {
        result.Summary.DependencyGraph = graphBuilder.String()
        utils.LogWithLocation(utils.Info, "Dependency analysis complete.")
    } else {
        utils.LogWithLocation(utils.Info, "No supported dependency files found (go.mod, requirements.txt).")
    }
}

// parseGoMod reads a go.mod file and extracts direct dependencies.
func parseGoMod(path string) ([]string, error) {
    content, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    modFile, err := modfile.Parse(path, content, nil)
    if err != nil {
        return nil, err
    }

    var deps []string
    for _, require := range modFile.Require {
        if !require.Indirect {
            deps = append(deps, require.Mod.Path)
        }
    }
    return deps, nil
}

// parseRequirementsTxt reads a requirements.txt file and extracts dependencies.
func parseRequirementsTxt(path string) ([]string, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    var deps []string
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        line := strings.TrimSpace(scanner.Text())
        if line != "" && !strings.HasPrefix(line, "#") {
            // Simple parsing, ignores version specifiers for clarity in the graph
            dep := strings.Split(line, "==")[0]
            dep = strings.Split(dep, ">=")[0]
            dep = strings.Split(dep, "<=")[0]
            deps = append(deps, strings.TrimSpace(dep))
        }
    }
    return deps, scanner.Err()
}

func (re *ReviewEngine) exportResults(result *models.AnalysisResult, outputDir string, formats []string) {
    for _, format := range formats {
        switch format {
        case "json":
            jsonPath := filepath.Join(outputDir, fmt.Sprintf("code-review-%s.json", result.ID))
            if err := utils.FormatToJSON(result, jsonPath); err != nil {
                utils.LogWithLocation(utils.Error, "Failed to export results to JSON: %v", err)
                continue
            }
            result.OutputFiles = append(result.OutputFiles, models.OutputFile{
                Format: "json",
                Path:   jsonPath,
            })
            
        case "markdown":
            mdPath := filepath.Join(outputDir, fmt.Sprintf("code-review-%s.md", result.ID))
            md, err := utils.FormatToMarkdown(result, mdPath)
            if err != nil {
                utils.LogWithLocation(utils.Error, "Failed to export results to Markdown: %v", err)
                continue
            }
            result.OutputFiles = append(result.OutputFiles, models.OutputFile{
                Format: "markdown",
                Path:   mdPath,
            })
           
            if containsFormat(formats, "pdf") {
                pdfPath := filepath.Join(outputDir, fmt.Sprintf("code-review-%s.pdf", result.ID))
                if err := utils.FormatToPDF(md, pdfPath); err != nil {
                    utils.LogWithLocation(utils.Error, "Failed to export results to PDF: %v", err)
                    continue
                }
                result.OutputFiles = append(result.OutputFiles, models.OutputFile{
                    Format: "pdf",
                    Path:   pdfPath,
                })
            }
        }
    }
}

func containsFormat(formats []string, format string) bool {
    for _, f := range formats {
        if f == format {
            return true
        }
    }
    return false
}
