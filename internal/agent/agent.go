package agent

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/euclidstellar/gollora/internal/ai"
	"github.com/euclidstellar/gollora/internal/models"
	"github.com/euclidstellar/gollora/internal/tools"
	"github.com/euclidstellar/gollora/internal/utils"
)

const (
	chunkSize    = 512   // characters
	chunkOverlap = 50    // characters
	topK         = 5     // Number of chunks to retrieve
)

// ProgressCallback is a function type for reporting progress.
type ProgressCallback func(message string)

// CachedData holds the indexed data and the commit hash it corresponds to.
type CachedData struct {
	CommitHash  string
	VectorStore []Chunk
}

// Chunk represents a piece of a document with its vector embedding.
type Chunk struct {
	Path      string
	Content   string
	Embedding []float32
}

// Agent handles the logic for the interactive Q&A.
type Agent struct {
	aiClient    *ai.Client
	vectorStore []Chunk
	astTool     *tools.ASTTool
	repoPath    string
}

// NewAgent creates and initializes a new Q&A agent by indexing the codebase.
func NewAgent(ctx context.Context, config *models.Config, repoPath string, progressCb ProgressCallback) (*Agent, error) {
    agent := &Agent{
        aiClient: ai.NewClient(config),
        astTool:  tools.NewASTTool(repoPath),
        repoPath: repoPath,
    }

    stateHash, err := utils.GetRepoStateHash(repoPath)
    if err != nil {
        return nil, fmt.Errorf("could not determine repository state: %v", err)
    }

    cachePath, err := getCachePath(repoPath)
    if err != nil {
        return nil, fmt.Errorf("could not determine cache path: %v", err)
    }

    // Try to load from cache
    if cachedData, err := loadCache(cachePath); err == nil && cachedData.CommitHash == stateHash {
        if progressCb != nil {
            progressCb("Loaded repository index from cache.")
        }
        agent.vectorStore = cachedData.VectorStore
        return agent, nil
    }

    // If cache is invalid or missing, re-index
    if progressCb != nil {
        progressCb("Creating new index for the repository...")
    }
    err = agent.index(ctx, repoPath, progressCb)
    if err != nil {
        return nil, fmt.Errorf("failed to index repository: %v", err)
    }

    // Save the newly indexed data to cache
    err = saveCache(cachePath, CachedData{CommitHash: stateHash, VectorStore: agent.vectorStore})
    if err != nil {
        utils.LogWithLocation(utils.Warn, "Failed to save index to cache: %v", err)
    }

    return agent, nil
}

// index walks through the repository, chunks files, and creates embeddings.
func (a *Agent) index(ctx context.Context, repoPath string, progressCb ProgressCallback) error {
    return filepath.Walk(repoPath, func(path string, info os.FileInfo, err error) error {
        if err != nil {
            return err
        }
        if info.IsDir() || !isTextFile(path) {
			return nil
		}

		relPath, err := filepath.Rel(repoPath, path)
		if err != nil {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			utils.LogWithLocation(utils.Warn, "Failed to read file %s: %v", relPath, err)
			return nil
		}

		chunks := splitIntoChunks(string(content))
		for _, chunkContent := range chunks {
			embedding, err := a.aiClient.GenerateEmbeddings(ctx, chunkContent)
			if err != nil {
				utils.LogWithLocation(utils.Warn, "Failed to generate embedding for chunk in %s: %v", relPath, err)
				continue // Skip this chunk
			}
			a.vectorStore = append(a.vectorStore, Chunk{
				Path:      relPath,
				Content:   chunkContent,
				Embedding: embedding,
			})
		}
        if progressCb != nil {
            progressCb(fmt.Sprintf("Indexed: %s", relPath))
        }
        return nil
    })
}

// Ask takes a user question, routes it to the correct tool, and generates an answer.
func (a *Agent) Ask(ctx context.Context, question string) (string, error) {
    toolChoice, err := a.routeToTool(ctx, question)
    if err != nil {
        // Fallback to RAG if routing fails
        return a.answerWithRAG(ctx, question)
    }

    switch toolChoice.Tool {
    case "ast_tool":
        return a.answerWithAST(ctx, toolChoice.Query, toolChoice.FilePath, question)
    case "vector_search":
        fallthrough
    default:
        return a.answerWithRAG(ctx, question)
    }
}

type toolChoice struct {
    Tool     string `json:"tool"`
    Query    string `json:"query,omitempty"`
    FilePath string `json:"file_path,omitempty"`
    Reason   string `json:"reason"`
}

func (a *Agent) routeToTool(ctx context.Context, question string) (*toolChoice, error) {
    prompt := fmt.Sprintf(`You are a router that decides which tool to use to answer a user's question about a codebase.

Available tools:
1.  **vector_search**: Use for general questions about what code does, how it works, or for finding information across multiple files.
2.  **ast_tool**: Use for specific questions about the structure of a **single Go file**. This is best for queries like "find all http handlers in main.go" or "list global variables in cmd/server.go".

User question: "%s"

You must respond with a JSON object indicating the best tool.
If using 'ast_tool', you must also provide the 'query' and 'file_path'.
Valid queries for 'ast_tool' are: 'find_http_handlers', 'find_global_variables'.
Extract the file path from the user's question.

Example responses:
- For "What does the WebhookHandler do?": {"tool": "vector_search", "reason": "This is a general question about functionality."}
- For "Show me all the HTTP handlers in cmd/webhook.go": {"tool": "ast_tool", "query": "find_http_handlers", "file_path": "cmd/webhook.go", "reason": "This is a specific structural query about a Go file."}

Your response:`, question)

    response, err := a.aiClient.GenerateContent(ctx, prompt)
    if err != nil {
        return nil, err
    }

    var choice toolChoice
    // Clean the response from markdown backticks
    response = strings.TrimPrefix(response, "```json")
    response = strings.TrimSuffix(response, "```")
    if err := json.Unmarshal([]byte(response), &choice); err != nil {
        return nil, fmt.Errorf("failed to decode tool choice: %v. Response: %s", err, response)
    }

    return &choice, nil
}

func (a *Agent) answerWithAST(ctx context.Context, query, filePath, originalQuestion string) (string, error) {
    toolResult, err := a.astTool.Execute(query, filePath)
    if err != nil {
        // AST tool failed, so fall back to the RAG method.
        fallbackMessage := fmt.Sprintf("I tried to use the AST tool but encountered an error: %v. I will try to answer using a general search instead.\n\n", err)
        ragAnswer, ragErr := a.answerWithRAG(ctx, originalQuestion)
        if ragErr != nil {
            // If RAG also fails, return a combined error.
            return "", fmt.Errorf("AST tool failed (%v) and fallback RAG also failed (%v)", err, ragErr)
        }
        // If RAG succeeds, prepend the fallback message to the answer.
        return fallbackMessage + ragAnswer, nil
    }

    prompt := fmt.Sprintf(`You are a helpful AI assistant. A user asked a question, and an automated tool was run on the codebase to get a result. Your job is to present this result to the user in a clear, natural way.

User's original question: "%s"
Tool result:
%s

Your friendly response:`, originalQuestion, toolResult)

    return a.aiClient.GenerateContent(ctx, prompt)
}

func (a *Agent) answerWithRAG(ctx context.Context, question string) (string, error) {
    questionEmbedding, err := a.aiClient.GenerateEmbeddings(ctx, question)
    if err != nil {
        return "", fmt.Errorf("failed to generate embedding for question: %v", err)
    }

    retrievedChunks := a.retrieveChunks(questionEmbedding, topK)
    if len(retrievedChunks) == 0 {
        return "I couldn't find any relevant information in the codebase to answer your question.", nil
    }

    var contextBuilder strings.Builder
    for _, chunk := range retrievedChunks {
        contextBuilder.WriteString(fmt.Sprintf("--- From file: %s ---\n%s\n\n", chunk.Path, chunk.Content))
    }

    prompt := fmt.Sprintf(`
You are a helpful AI assistant with expertise in software engineering.
Answer the following question based on the provided code context.
Provide clear, concise answers. If the context is insufficient, say so.

CONTEXT:
%s

QUESTION:
%s

ANSWER:`, contextBuilder.String(), question)

    answer, err := a.aiClient.GenerateContent(ctx, prompt)
    if err != nil {
        return "", fmt.Errorf("failed to generate answer from AI: %v", err)
    }

    return answer, nil
}

// retrieveChunks finds the most relevant chunks from the vector store.
func (a *Agent) retrieveChunks(questionEmbedding []float32, k int) []Chunk {
	type scoredChunk struct {
		Chunk
		Score float64
	}

	scored := make([]scoredChunk, len(a.vectorStore))
	for i, chunk := range a.vectorStore {
		scored[i] = scoredChunk{
			Chunk: chunk,
			Score: cosineSimilarity(questionEmbedding, chunk.Embedding),
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	limit := k
	if len(scored) < k {
		limit = len(scored)
	}

	result := make([]Chunk, limit)
	for i := 0; i < limit; i++ {
		result[i] = scored[i].Chunk
	}
	return result
}

func getCachePath(repoPath string) (string, error) {
    absRepoPath, err := filepath.Abs(repoPath)
    if err != nil {
        return "", err
    }

    homeDir, err := os.UserHomeDir()
    if err != nil {
        return "", err
    }

    cacheDir := filepath.Join(homeDir, ".gollora", "cache")
    if err := os.MkdirAll(cacheDir, 0755); err != nil {
        return "", err
    }

    hasher := sha256.New()
    hasher.Write([]byte(absRepoPath))
    cacheFileName := hex.EncodeToString(hasher.Sum(nil)) + ".gob"

    return filepath.Join(cacheDir, cacheFileName), nil
}

func saveCache(filePath string, data CachedData) error {
    file, err := os.Create(filePath)
    if err != nil {
        return err
    }
    defer file.Close()

    encoder := gob.NewEncoder(file)
    return encoder.Encode(data)
}

func loadCache(filePath string) (CachedData, error) {
    var data CachedData
    file, err := os.Open(filePath)
    if err != nil {
        return data, err
    }
    defer file.Close()

    decoder := gob.NewDecoder(file)
    err = decoder.Decode(&data)
    return data, err
}

// Helper functions
func isTextFile(path string) bool {
    // Exclude common system files by name
    basename := filepath.Base(path)
    if basename == ".DS_Store" {
        return false
    }

    // A simple check to exclude common binary files
    ext := strings.ToLower(filepath.Ext(path))
    switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".zip", ".tar", ".gz", ".exe", ".bin", ".so", ".dll", ".pdf":
		return false
	}
	// Exclude .git directory
	if strings.Contains(path, ".git"+string(filepath.Separator)) {
		return false
	}
	return true
}

func splitIntoChunks(text string) []string {
	var chunks []string
	textRunes := []rune(text)
	if len(textRunes) == 0 {
		return chunks
	}

	for i := 0; i < len(textRunes); i += (chunkSize - chunkOverlap) {
		end := i + chunkSize
		if end > len(textRunes) {
			end = len(textRunes)
		}
		chunks = append(chunks, string(textRunes[i:end]))
	}
	return chunks
}

func cosineSimilarity(a, b []float32) float64 {
    var dotProduct, normA, normB float64
    for i := 0; i < len(a); i++ {
        dotProduct += float64(a[i] * b[i])
        normA += float64(a[i] * a[i])
        normB += float64(b[i] * b[i])
    }
    if normA == 0 || normB == 0 {
        return 0.0
    }
    return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

