package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sirtheprogrammer/docker-deployments/server/internal/ai"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/auth"
	"github.com/sirtheprogrammer/docker-deployments/server/internal/store"
)

// --- Settings ---

func (s *Server) handleGetAISettings(w http.ResponseWriter, r *http.Request) error {
	user := MustIdentity(r.Context()).User
	settings, err := s.Store.GetAISettings(r.Context(), user.ID)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, settings)
}

type updateAISettingsRequest struct {
	Provider           string  `json:"provider"`
	Model              string  `json:"model"`
	BaseURL            string  `json:"base_url"`
	APIKey             string  `json:"api_key"`
	Temperature        float64 `json:"temperature"`
	SystemPromptCustom string  `json:"system_prompt_custom"`
}

func (s *Server) handleUpdateAISettings(w http.ResponseWriter, r *http.Request) error {
	var req updateAISettingsRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	user := MustIdentity(r.Context()).User

	if req.Temperature <= 0 || req.Temperature > 2.0 {
		req.Temperature = 0.7
	}

	updated, err := s.Store.UpsertAISettings(
		r.Context(),
		s.Sealer,
		user.ID,
		req.Provider,
		req.Model,
		req.BaseURL,
		req.APIKey,
		req.SystemPromptCustom,
		req.Temperature,
	)
	if err != nil {
		return Internal(err)
	}

	AuditMeta(r.Context(), "provider", req.Provider)
	AuditMeta(r.Context(), "model", req.Model)
	return JSON(w, s.Log, http.StatusOK, updated)
}

type testAIConnectionRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
}

func (s *Server) handleTestAIConnection(w http.ResponseWriter, r *http.Request) error {
	var req testAIConnectionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	user := MustIdentity(r.Context()).User

	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		// Use stored API key
		storedKey, err := s.Store.GetAIAPIKey(r.Context(), s.Sealer, user.ID)
		if err != nil {
			return Internal(err)
		}
		apiKey = storedKey
	}

	if apiKey == "" && strings.ToLower(req.Provider) != "custom" {
		return BadRequest("No API key provided. Please enter an API key or save one in settings first.")
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	opts := ai.StreamOptions{
		Provider:    req.Provider,
		Model:       req.Model,
		BaseURL:     req.BaseURL,
		APIKey:      apiKey,
		Temperature: 0.1,
	}

	res, err := s.AI.Client.TestConnection(ctx, opts)
	if err != nil {
		return BadRequest("Provider connection test failed: %v", err)
	}

	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"status":   "ok",
		"message":  "Connection successful! Provider responded: " + res,
		"provider": req.Provider,
		"model":    req.Model,
	})
}

// --- Conversations ---

func (s *Server) handleListAIConversations(w http.ResponseWriter, r *http.Request) error {
	user := MustIdentity(r.Context()).User
	convs, err := s.Store.ListAIConversations(r.Context(), user.ID)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusOK, map[string]any{"conversations": convs})
}

type createAIConversationRequest struct {
	Title string `json:"title"`
}

func (s *Server) handleCreateAIConversation(w http.ResponseWriter, r *http.Request) error {
	var req createAIConversationRequest
	_ = DecodeJSON(w, r, &req)

	user := MustIdentity(r.Context()).User
	conv, err := s.Store.CreateAIConversation(r.Context(), user.ID, req.Title)
	if err != nil {
		return Internal(err)
	}
	return JSON(w, s.Log, http.StatusCreated, conv)
}

func (s *Server) handleGetAIConversation(w http.ResponseWriter, r *http.Request) error {
	convID := chi.URLParam(r, "conversationID")
	user := MustIdentity(r.Context()).User

	conv, msgs, err := s.Store.GetAIConversation(r.Context(), convID, user.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such conversation.")
		}
		return Internal(err)
	}

	return JSON(w, s.Log, http.StatusOK, map[string]any{
		"conversation": conv,
		"messages":     msgs,
	})
}

func (s *Server) handleDeleteAIConversation(w http.ResponseWriter, r *http.Request) error {
	convID := chi.URLParam(r, "conversationID")
	user := MustIdentity(r.Context()).User

	err := s.Store.DeleteAIConversation(r.Context(), convID, user.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return NotFound("No such conversation.")
		}
		return Internal(err)
	}
	return NoContent(w)
}

// --- Chat Streaming ---

type aiChatRequest struct {
	ConversationID string `json:"conversation_id"`
	Message        string `json:"message"`
	ServerID       string `json:"server_id,omitempty"`
	DeploymentID   string `json:"deployment_id,omitempty"`
	ContainerID    string `json:"container_id,omitempty"`
}

type aiEventWriter struct {
	mu      sync.Mutex
	w       http.ResponseWriter
	flusher http.Flusher
}

func (e *aiEventWriter) event(name string, payload any) {
	e.mu.Lock()
	defer e.mu.Unlock()

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", name, data)
	e.flusher.Flush()
}

var safeguardRegex = regexp.MustCompile("(?s)```safeguard_action\\s*(\\{.*?\\})\\s*```")

func (s *Server) handleAIChat(w http.ResponseWriter, r *http.Request) error {
	var req aiChatRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		return err
	}

	if strings.TrimSpace(req.Message) == "" {
		return BadRequest("Message content cannot be empty.")
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		return Internal(fmt.Errorf("api: streaming not supported by response writer"))
	}

	user := MustIdentity(r.Context()).User

	// Check Server access if provided
	if req.ServerID != "" {
		allowed, err := s.Store.CanAccessServer(r.Context(), req.ServerID, user.ID, user.Role == auth.RoleAdmin)
		if err != nil || !allowed {
			return Forbidden("You do not have access to the specified server.")
		}
	}

	// Fetch user's AI settings & API key
	settings, err := s.Store.GetAISettings(r.Context(), user.ID)
	if err != nil {
		return Internal(err)
	}

	apiKey, err := s.Store.GetAIAPIKey(r.Context(), s.Sealer, user.ID)
	if err != nil {
		return Internal(err)
	}

	if apiKey == "" && settings.Provider != "custom" {
		return BadRequest("No API key configured for provider '%s'. Please set one in Settings -> AI Assistant.", settings.Provider)
	}

	// Get or create conversation
	var conv *store.AIConversation
	if req.ConversationID != "" {
		existingConv, _, err := s.Store.GetAIConversation(r.Context(), req.ConversationID, user.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return NotFound("No such conversation.")
			}
			return Internal(err)
		}
		conv = existingConv
	} else {
		title := req.Message
		if len(title) > 40 {
			title = title[:40] + "..."
		}
		createdConv, err := s.Store.CreateAIConversation(r.Context(), user.ID, title)
		if err != nil {
			return Internal(err)
		}
		conv = createdConv
	}

	// Save User Message in Store
	userMeta := map[string]any{}
	if req.ServerID != "" {
		userMeta["server_id"] = req.ServerID
	}
	if req.DeploymentID != "" {
		userMeta["deployment_id"] = req.DeploymentID
	}
	if req.ContainerID != "" {
		userMeta["container_id"] = req.ContainerID
	}
	metaBytes, _ := json.Marshal(userMeta)
	_, err = s.Store.AddAIMessage(r.Context(), conv.ID, "user", req.Message, metaBytes)
	if err != nil {
		return Internal(err)
	}

	// Prepare Context
	contextInfo, _ := s.AI.PrepareContext(r.Context(), req.ServerID, req.DeploymentID)

	// Fetch recent history
	_, history, _ := s.Store.GetAIConversation(r.Context(), conv.ID, user.ID)
	chatMessages := []ai.ChatMessage{}
	for _, h := range history {
		chatMessages = append(chatMessages, ai.ChatMessage{
			Role:    h.Role,
			Content: h.Content,
		})
	}

	// Build System Prompt
	fullSystemPrompt := ai.BaseSystemPrompt
	if settings.SystemPromptCustom != "" {
		fullSystemPrompt += "\n\nUser Custom Instructions:\n" + settings.SystemPromptCustom
	}
	if contextInfo != "" {
		fullSystemPrompt += "\n\nCurrent System Context:\n" + contextInfo
	}

	// Prepare SSE response headers
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	writer := &aiEventWriter{w: w, flusher: flusher}
	writer.event("conversation", conv)

	var fullResponse strings.Builder
	opts := ai.StreamOptions{
		Provider:     settings.Provider,
		Model:        settings.Model,
		BaseURL:      settings.BaseURL,
		APIKey:       apiKey,
		Temperature:  settings.Temperature,
		SystemPrompt: fullSystemPrompt,
		Messages:     chatMessages,
	}

	err = s.AI.Client.StreamCompletion(r.Context(), opts, func(chunk string) error {
		fullResponse.WriteString(chunk)
		writer.event("chunk", map[string]string{"text": chunk})
		return nil
	})

	if err != nil {
		if r.Context().Err() == nil {
			s.Log.Warn("ai stream error", "error", err)
			writer.event("error", map[string]string{"message": err.Error()})
		}
		return nil
	}

	assistantReply := fullResponse.String()

	// Check for safeguard action
	var assistantMetadata json.RawMessage
	match := safeguardRegex.FindStringSubmatch(assistantReply)
	if len(match) > 1 {
		var action ai.SafeguardAction
		if err := json.Unmarshal([]byte(match[1]), &action); err == nil {
			if action.ID == "" {
				action.ID = fmt.Sprintf("act-%d", time.Now().UnixNano())
			}
			writer.event("action", action)
			metaObj := map[string]any{"safeguard_action": action}
			assistantMetadata, _ = json.Marshal(metaObj)
		}
	}

	// Save Assistant Reply
	msg, saveErr := s.Store.AddAIMessage(r.Context(), conv.ID, "assistant", assistantReply, assistantMetadata)
	if saveErr == nil && msg != nil {
		writer.event("done", map[string]any{"message_id": msg.ID, "conversation_id": conv.ID})
	} else {
		writer.event("done", map[string]any{"conversation_id": conv.ID})
	}

	return nil
}

func (s *Server) handleAIDiagnoseServer(w http.ResponseWriter, r *http.Request) error {
	server, err := s.requireServer(r)
	if err != nil {
		return err
	}

	containerID := r.URL.Query().Get("container_id")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	diag, err := ai.CollectServerDiagnostic(ctx, s.Servers, server, containerID)
	if err != nil {
		return Unavailable("Could not collect server diagnostic: %v", err).WithCause(err)
	}

	return JSON(w, s.Log, http.StatusOK, diag)
}
