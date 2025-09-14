package utils

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

func FormatToJSON(data interface{}, outputFile string) error {
    jsonData, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal JSON: %v", err)
    }

    if outputFile != "" {
        if err := ioutil.WriteFile(outputFile, jsonData, 0644); err != nil {
            return fmt.Errorf("failed to write JSON file: %v", err)
        }
    }

    return nil
}

func FormatToMarkdown(data interface{}, outputFile string) (string, error) {
    jsonData, err := json.Marshal(data)
    if err != nil {
        return "", fmt.Errorf("failed to marshal JSON: %v", err)
    }

    var results map[string]interface{}
    if err := json.Unmarshal(jsonData, &results); err != nil {
        return "", fmt.Errorf("failed to unmarshal JSON: %v", err)
    }

    var md strings.Builder
    md.WriteString("# Code Review Report\n\n")
    md.WriteString("## Summary\n\n")

    if summary, ok := results["summary"].(map[string]interface{}); ok {
        md.WriteString("| Metric | Value |\n")
        md.WriteString("|--------|-------|\n")
        for k, v := range summary {
            if k != "dependency_graph" { // Don't show the graph string in the summary table
                md.WriteString(fmt.Sprintf("| %s | %v |\n", strings.Title(k), v))
            }
        }
    }

    // Add Dependency Graph if it exists
    if summary, ok := results["summary"].(map[string]interface{}); ok {
        if graph, ok := summary["dependency_graph"].(string); ok && graph != "" {
            md.WriteString("\n## Dependency Graph\n\n")
            md.WriteString("```mermaid\n")
            md.WriteString(graph)
            md.WriteString("```\n")
        }
    }

    md.WriteString("\n## Findings\n\n")

    if issues, ok := results["issues"].([]interface{}); ok {
        for i, issue := range issues {
            issueMap, _ := issue.(map[string]interface{})
            md.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, issueMap["title"]))
            md.WriteString(fmt.Sprintf("**Severity**: %s  \n", issueMap["severity"]))
            md.WriteString(fmt.Sprintf("**File**: %s  \n", issueMap["file"]))
            md.WriteString(fmt.Sprintf("**Line**: %v  \n\n", issueMap["line"]))
            md.WriteString(fmt.Sprintf("**Description**: %s  \n\n", issueMap["description"]))
            
            if code, ok := issueMap["code"].(string); ok && code != "" {
                md.WriteString("```\n")
                md.WriteString(code)
                md.WriteString("\n```\n\n")
            }
            
            if fix, ok := issueMap["fix"].(string); ok && fix != "" {
                md.WriteString("**Suggested Fix**:  \n\n")
                md.WriteString("```\n")
                md.WriteString(fix)
                md.WriteString("\n```\n\n")
            }
        }
    }

    markdownContent := md.String()
    
    if outputFile != "" {
        if err := ioutil.WriteFile(outputFile, []byte(markdownContent), 0644); err != nil {
            return markdownContent, fmt.Errorf("failed to write Markdown file: %v", err)
        }
    }

    return markdownContent, nil
}

func FormatToPDF(markdownContent, outputFile string) error {
    // Create temporary markdown file
    tmpDir, err := ioutil.TempDir("", "gollora")
    if err != nil {
        return fmt.Errorf("failed to create temp directory: %v", err)
    }
    defer os.RemoveAll(tmpDir)
    
    mdFile := filepath.Join(tmpDir, "report.md")
    htmlFile := filepath.Join(tmpDir, "report.html")
    
    if err := ioutil.WriteFile(mdFile, []byte(markdownContent), 0644); err != nil {
        return fmt.Errorf("failed to write markdown file: %v", err)
    }
    
    // Convert markdown to HTML
    mdParser := parser.NewWithExtensions(parser.CommonExtensions | parser.AutoHeadingIDs)
    mdBytes := []byte(markdownContent)
    parsedMd := mdParser.Parse(mdBytes)
    
    htmlFlags := html.CommonFlags | html.HrefTargetBlank
    opts := html.RendererOptions{Flags: htmlFlags}
    renderer := html.NewRenderer(opts)
    
    htmlContent := markdown.Render(parsedMd, renderer)
    
    // Add basic styling to the HTML
    styledHTML := []byte(fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; max-width: 800px; margin: 0 auto; padding: 20px; }
        pre { background-color: #f5f5f5; padding: 10px; border-radius: 5px; overflow-x: auto; }
        code { font-family: Consolas, Monaco, 'Andale Mono', monospace; }
        table { border-collapse: collapse; width: 100%%; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #f2f2f2; }
        h1, h2, h3 { color: #333; }
    </style>
    <title>Code Review Report</title>
</head>
<body>
    %s
</body>
</html>
`, string(htmlContent)))
    
    if err := ioutil.WriteFile(htmlFile, styledHTML, 0644); err != nil {
        return fmt.Errorf("failed to write HTML file: %v", err)
    }
    
    cmd := exec.Command("wkhtmltopdf", htmlFile, outputFile)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to generate PDF: %v", err)
    }
    
    return nil
}