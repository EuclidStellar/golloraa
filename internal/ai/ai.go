package ai

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "github.com/euclidstellar/gollora/internal/models"
)

const (
    geminiAPIURL   = "https://generativelanguage.googleapis.com/v1beta/models"
    embeddingModel = "text-embedding-004" // Using the latest embedding model
)

// Client handles communication with the AI provider's API.
type Client struct {
    Provider       string
    APIKey         string
    Model          string
    EmbeddingModel string
    httpClient     *http.Client
}

// NewClient creates a new AI client.
func NewClient(config *models.Config) *Client {
    return &Client{
        Provider:       config.AI.Provider,
        APIKey:         config.AI.APIKey,
        Model:          config.AI.Model,
        EmbeddingModel: embeddingModel,
        httpClient:     &http.Client{Timeout: 60 * time.Second},
    }
}

// GenerateEmbeddings creates vector embeddings for a given text.
func (c *Client) GenerateEmbeddings(ctx context.Context, text string) ([]float32, error) {
    if c.Provider != "gemini" {
        return nil, fmt.Errorf("unsupported AI provider for embeddings: %s", c.Provider)
    }

    endpoint := fmt.Sprintf("%s/%s:embedContent?key=%s", geminiAPIURL, c.EmbeddingModel, c.APIKey)

    reqBody := map[string]interface{}{
        "model": "models/" + c.EmbeddingModel,
        "content": map[string]interface{}{
            "parts": []map[string]string{{"text": text}},
        },
    }
    jsonBody, err := json.Marshal(reqBody)
    if err != nil {
        return nil, fmt.Errorf("failed to marshal embedding request: %v", err)
    }

    req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonBody))
    if err != nil {
        return nil, fmt.Errorf("failed to create embedding request: %v", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to send embedding request: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("gemini embedding API error: %s - %s", resp.Status, string(body))
    }

    var result struct {
        Embedding struct {
            Value []float32 `json:"values"`
        } `json:"embedding"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, fmt.Errorf("failed to decode embedding response: %v", err)
    }

    return result.Embedding.Value, nil
}

// GenerateContent asks the AI model a question with provided context.
func (c *Client) GenerateContent(ctx context.Context, prompt string) (string, error) {
    if c.Provider != "gemini" {
        return "", fmt.Errorf("unsupported AI provider for content generation: %s", c.Provider)
    }

    endpoint := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiAPIURL, c.Model, c.APIKey)

    reqBody := map[string]interface{}{
        "contents": []map[string]interface{}{
            {"parts": []map[string]string{{"text": prompt}}},
        },
    }
    jsonBody, err := json.Marshal(reqBody)
    if err != nil {
        return "", fmt.Errorf("failed to marshal content request: %v", err)
    }

    req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBuffer(jsonBody))
    if err != nil {
        return "", fmt.Errorf("failed to create content request: %v", err)
    }
    req.Header.Set("Content-Type", "application/json")

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return "", fmt.Errorf("failed to send content request: %v", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return "", fmt.Errorf("gemini content API error: %s - %s", resp.Status, string(body))
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
        return "", fmt.Errorf("failed to decode content response: %v", err)
    }

    if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
        return result.Candidates[0].Content.Parts[0].Text, nil
    }

    return "", fmt.Errorf("no content returned from AI")
}