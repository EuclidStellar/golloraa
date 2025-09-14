package tools

import (
    "fmt"
    "go/ast"
    "go/parser"
    "go/token"
    "os"
    "path/filepath"
    "strings"
)

// ASTTool provides methods to analyze Go source code using AST.
type ASTTool struct {
    repoPath string
}

// NewASTTool creates a new ASTTool.
func NewASTTool(repoPath string) *ASTTool {
    return &ASTTool{repoPath: repoPath}
}

// Execute runs a specific query against the Go AST.
func (t *ASTTool) Execute(query, filePath string) (string, error) {
    fullPath := filepath.Join(t.repoPath, filePath)
    if _, err := os.Stat(fullPath); os.IsNotExist(err) {
        return "", fmt.Errorf("file not found: %s", filePath)
    }

    fset := token.NewFileSet()
    node, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
    if err != nil {
        return "", fmt.Errorf("failed to parse file %s: %v", filePath, err)
    }

    switch query {
    case "find_http_handlers":
        return t.findHTTPHandlers(node), nil
    case "find_global_variables":
        return t.findGlobalVariables(node), nil
    default:
        return "", fmt.Errorf("unknown AST query: %s", query)
    }
}

func (t *ASTTool) findHTTPHandlers(node *ast.File) string {
    var handlers []string
    ast.Inspect(node, func(n ast.Node) bool {
        call, ok := n.(*ast.CallExpr)
        if !ok {
            return true
        }

        // Look for http.HandleFunc("pattern", handler)
        if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
            if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "http" && sel.Sel.Name == "HandleFunc" {
                if len(call.Args) == 2 {
                    pattern := t.getExprString(call.Args[0])
                    handlerName := t.getExprString(call.Args[1])
                    handlers = append(handlers, fmt.Sprintf("- Pattern: %s, Handler: %s", pattern, handlerName))
                }
            }
        }
        return true
    })

    if len(handlers) == 0 {
        return "No HTTP handlers found using `http.HandleFunc`."
    }
    return "Found the following HTTP handlers:\n" + strings.Join(handlers, "\n")
}

func (t *ASTTool) findGlobalVariables(node *ast.File) string {
    var globals []string
    for _, decl := range node.Decls {
        if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.VAR {
            for _, spec := range genDecl.Specs {
                if valueSpec, ok := spec.(*ast.ValueSpec); ok {
                    for _, name := range valueSpec.Names {
                        globals = append(globals, fmt.Sprintf("- %s", name.Name))
                    }
                }
            }
        }
    }

    if len(globals) == 0 {
        return "No global variables found."
    }
    return "Found the following global variables:\n" + strings.Join(globals, "\n")
}

func (t *ASTTool) getExprString(expr ast.Expr) string {
    switch v := expr.(type) {
    case *ast.BasicLit:
        return v.Value
    case *ast.Ident:
        return v.Name
    default:
        return "complex_expression"
    }
}