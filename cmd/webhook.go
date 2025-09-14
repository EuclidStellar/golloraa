package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/euclidstellar/gollora/internal/agent"
	"github.com/euclidstellar/gollora/internal/models"
	"github.com/euclidstellar/gollora/internal/utils"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all origins
}

type WebhookHandler struct {
	config      *models.Config
	toolsConfig *models.AnalysisToolsConfig
}

func NewWebhookHandler(config *models.Config, toolsConfig *models.AnalysisToolsConfig) *WebhookHandler {
	return &WebhookHandler{
		config:      config,
		toolsConfig: toolsConfig,
	}
}

func (wh *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	switch {
	case path == "webhook/github" || path == "webhook/github/":
		wh.handleGitHubWebhook(w, r)
	case path == "qa":
		wh.handleQAPage(w, r)
	case path == "qa/ws":
		wh.handleQAWebsocket(w, r)
	case path == "analyze":
		wh.handleAnalyzePage(w, r)
	case path == "analyze/ws":
		wh.handleAnalyzeWebsocket(w, r)
	case path == "webhook/gitlab" || path == "webhook/gitlab/":
		wh.handleGitLabWebhook(w, r)
	case path == "webhook/bitbucket" || path == "webhook/bitbucket/":
		wh.handleBitbucketWebhook(w, r)
	case path == "webhook" || path == "webhook/":
		wh.handleGenericWebhook(w, r)
	case path == "health" || path == "health/":
		wh.handleHealthCheck(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (wh *WebhookHandler) handleAnalyzePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/analyze.html")
}

func (wh *WebhookHandler) handleAnalyzeWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.LogWithLocation(utils.Error, "Failed to upgrade websocket for analysis: %v", err)
		return
	}
	defer conn.Close()

	sendMessage := func(msgType string, message string) {
		conn.WriteJSON(map[string]string{"type": msgType, "message": message})
	}

	// 1. Read init message
	_, msg, err := conn.ReadMessage()
	if err != nil {
		sendMessage("error", "Failed to read init message.")
		return
	}

	var initPayload struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(msg, &initPayload); err != nil || initPayload.Type != "init" {
		sendMessage("error", "Invalid init message.")
		return
	}

	// 2. Run the analysis process
	ctx := context.Background()
	repoFullName := extractRepoFullNameFromURL(initPayload.URL)
	event := models.WebhookEvent{
		Type:         "on-demand-scan",
		Provider:     "web",
		RepoURL:      initPayload.URL,
		RepoFullName: repoFullName,
		HeadCommit:   "HEAD", // Analyze the latest commit
	}

	// This is a simplified analysis run. We'll call the core components directly.
	fetcher := NewCodeFetcher()
	engine := NewReviewEngine(wh.config, wh.toolsConfig)

	sendMessage("status", "Fetching repository...")
	repoPath, files, err := fetcher.FetchCode(ctx, event)
	if err != nil {
		sendMessage("error", fmt.Sprintf("Failed to fetch code: %v", err))
		return
	}
	defer os.RemoveAll(repoPath)

	request := models.AnalysisRequest{
		Event:    event,
		RepoPath: repoPath,
		Files:    files,
		Settings: models.AnalysisSettings{
			EnableAI:      wh.config.AI.Enabled,
			ExportFormats: []string{"markdown"}, // We'll generate a markdown report
		},
	}

	sendMessage("status", "Analyzing code... This may take a moment.")
	result, err := engine.Analyze(ctx, request)
	if err != nil {
		sendMessage("error", fmt.Sprintf("Analysis failed: %v", err))
		return
	}

	// 3. Generate and send the full JSON report
	resultJSON, err := json.Marshal(result)
	if err != nil {
		sendMessage("error", fmt.Sprintf("Failed to serialize result: %v", err))
		return
	}

	sendMessage("result", string(resultJSON))
}

func (wh *WebhookHandler) handleQAPage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/qa.html")
}

func (wh *WebhookHandler) handleQAWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		utils.LogWithLocation(utils.Error, "Failed to upgrade websocket: %v", err)
		return
	}
	defer conn.Close()

	sendMessage := func(msgType string, message string) {
		conn.WriteJSON(map[string]string{"type": msgType, "message": message})
	}

	// 1. Read init message with repo URL
	_, msg, err := conn.ReadMessage()
	if err != nil {
		sendMessage("error", "Failed to read init message.")
		return
	}

	var initPayload struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(msg, &initPayload); err != nil || initPayload.Type != "init" {
		sendMessage("error", "Invalid init message.")
		return
	}

	// 2. Clone the repository
	sendMessage("status", fmt.Sprintf("Cloning repository: %s", initPayload.URL))
	tempDir, err := os.MkdirTemp("", "gollora-qa-")
	if err != nil {
		sendMessage("error", "Failed to create temporary directory.")
		return
	}
	defer os.RemoveAll(tempDir)

	if err := utils.CloneRepository(tempDir, initPayload.URL, ""); err != nil {
		sendMessage("error", fmt.Sprintf("Failed to clone repository: %v", err))
		return
	}

	// 3. Initialize the agent (which will index the code)
	sendMessage("status", "Initializing agent...")
	ctx := context.Background()

	progressCallback := func(message string) {
		sendMessage("status", message)
	}

	qaAgent, err := agent.NewAgent(ctx, wh.config, tempDir, progressCallback)
	if err != nil {
		sendMessage("error", fmt.Sprintf("Failed to initialize agent: %v", err))
		return
	}

	sendMessage("ready", "Agent is ready. Ask your questions!")

	// 4. Loop to handle questions
	for {
		_, questionMsg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				utils.LogWithLocation(utils.Info, "Websocket closed: %v", err)
			}
			break
		}

		var questionPayload struct {
			Type     string `json:"type"`
			Question string `json:"question"`
		}
		if err := json.Unmarshal(questionMsg, &questionPayload); err != nil || questionPayload.Type != "question" {
			continue
		}

		sendMessage("status", "Thinking...")
		answer, err := qaAgent.Ask(ctx, questionPayload.Question)
		if err != nil {
			sendMessage("error", fmt.Sprintf("Failed to get answer: %v", err))
			continue
		}
		sendMessage("answer", answer)
	}
}

func (wh *WebhookHandler) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (wh *WebhookHandler) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if wh.config.GitHub.WebhookSecret != "" {
		signature := r.Header.Get("X-Hub-Signature-256")
		if signature == "" {
			signature = r.Header.Get("X-Hub-Signature")
		}

		if signature == "" {
			http.Error(w, "No signature provided", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
			return
		}
		r.Body.Close()

		if !wh.verifyGitHubSignature(signature, body, wh.config.GitHub.WebhookSecret) {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		r.Body = io.NopCloser(strings.NewReader(string(body)))
	}

	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		http.Error(w, "No event type provided", http.StatusBadRequest)
		return
	}

	if eventType != "push" && eventType != "pull_request" {
		utils.LogWithLocation(utils.Info, "Ignoring GitHub event type: %s", eventType)
		w.WriteHeader(http.StatusOK)
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Failed to parse webhook payload", http.StatusBadRequest)
		return
	}

	event, err := wh.extractGitHubEvent(eventType, payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to extract event details: %v", err), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		runAnalysisProcess(ctx, wh.config, wh.toolsConfig, event)
	}()
}

func (wh *WebhookHandler) handleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "GitLab webhook handling not implemented yet", http.StatusNotImplemented)
}

func (wh *WebhookHandler) handleBitbucketWebhook(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Bitbucket webhook handling not implemented yet", http.StatusNotImplemented)
}

func (wh *WebhookHandler) handleGenericWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Event models.WebhookEvent `json:"event"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Failed to parse webhook payload", http.StatusBadRequest)
		return
	}

	if payload.Event.Type == "" || payload.Event.RepoURL == "" ||
		payload.Event.BaseCommit == "" || payload.Event.HeadCommit == "" {
		http.Error(w, "Missing required event fields", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		runAnalysisProcess(ctx, wh.config, wh.toolsConfig, payload.Event)
	}()
}

func (wh *WebhookHandler) verifyGitHubSignature(signature string, payload []byte, secret string) bool {
	parts := strings.SplitN(signature, "=", 2)
	if len(parts) != 2 {
		return false
	}

	algorithm := parts[0]
	signatureHex := parts[1]

	var mac []byte

	switch algorithm {
	case "sha1":
		h := hmac.New(sha1.New, []byte(secret))
		h.Write(payload)
		mac = h.Sum(nil)
	case "sha256":
		h := hmac.New(sha256.New, []byte(secret))
		h.Write(payload)
		mac = h.Sum(nil)
	default:
		return false
	}

	expectedMAC, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false
	}

	return hmac.Equal(mac, expectedMAC)
}

func (wh *WebhookHandler) extractGitHubEvent(eventType string, payload map[string]interface{}) (models.WebhookEvent, error) {
	event := models.WebhookEvent{
		Type:     eventType,
		Provider: "github",
	}

	if repo, ok := payload["repository"].(map[string]interface{}); ok {
		if fullName, ok := repo["full_name"].(string); ok {
			event.RepoFullName = fullName
		}

		if htmlURL, ok := repo["html_url"].(string); ok {
			event.RepoURL = htmlURL + ".git"
		} else if cloneURL, ok := repo["clone_url"].(string); ok {
			event.RepoURL = cloneURL
		}

		if defaultBranch, ok := repo["default_branch"].(string); ok {
			event.Branch = defaultBranch
		}
	}

	if event.RepoFullName == "" || event.RepoURL == "" {
		return event, fmt.Errorf("missing repository information")
	}

	switch eventType {
	case "push":
		if before, ok := payload["before"].(string); ok {
			event.BaseCommit = before
		}

		if after, ok := payload["after"].(string); ok {
			event.HeadCommit = after
		}

		// For push events, extract the branch from the ref
		if ref, ok := payload["ref"].(string); ok {
			if strings.HasPrefix(ref, "refs/heads/") {
				event.Branch = strings.TrimPrefix(ref, "refs/heads/")
			}
		}

	case "pull_request":
		if pr, ok := payload["pull_request"].(map[string]interface{}); ok {
			if number, ok := payload["number"].(float64); ok {
				event.PullRequestID = int(number)
			}

			if htmlURL, ok := pr["html_url"].(string); ok {
				event.PullRequestURL = htmlURL
			}

			if base, ok := pr["base"].(map[string]interface{}); ok {
				if sha, ok := base["sha"].(string); ok {
					event.BaseCommit = sha
				}
			}

			if head, ok := pr["head"].(map[string]interface{}); ok {
				if sha, ok := head["sha"].(string); ok {
					event.HeadCommit = sha
				}

				// For pull_request events, extract the branch from the head
				if ref, ok := head["ref"].(string); ok {
					event.Branch = ref
				}
			}
		}
	}

	if event.BaseCommit == "" || event.HeadCommit == "" {
		return event, fmt.Errorf("missing commit information")
	}

	return event, nil
}