package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bright-guard/bright-guard/cloud/api/internal/auth"
	"github.com/bright-guard/bright-guard/cloud/api/internal/chat"
	"github.com/bright-guard/bright-guard/cloud/api/internal/store"
)

type createChatSessionResp struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
}

func (s *Server) handleCreateChatSession(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	sess, err := s.Chat.CreateSession(r.Context(), orgID, user.ID, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create session")
		return
	}
	writeJSON(w, http.StatusOK, createChatSessionResp{ID: sess.ID, Title: sess.Title})
}

type chatSessionRef struct {
	ID            uuid.UUID `json:"id"`
	Title         string    `json:"title"`
	TotalTokens   int64     `json:"totalTokens"`
	CreatedAt     time.Time `json:"createdAt"`
	LastMessageAt time.Time `json:"lastMessageAt"`
}

func (s *Server) handleListChatSessions(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	rows, err := s.Chat.ListSessions(r.Context(), orgID, user.ID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not list sessions")
		return
	}
	items := make([]chatSessionRef, 0, len(rows))
	for _, sess := range rows {
		items = append(items, chatSessionRef{
			ID: sess.ID, Title: sess.Title, TotalTokens: sess.TotalTokens,
			CreatedAt: sess.CreatedAt, LastMessageAt: sess.LastMessageAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

type chatThreadMsg struct {
	ID           uuid.UUID       `json:"id"`
	Role         string          `json:"role"`
	Content      json.RawMessage `json:"content"`
	InputTokens  int             `json:"inputTokens"`
	OutputTokens int             `json:"outputTokens"`
	ToolCalls    json.RawMessage `json:"toolCalls,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
}

func (s *Server) handleGetChatSession(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	sid, err := uuid.Parse(chi.URLParam(r, "sid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid session id")
		return
	}
	sess, err := s.Chat.GetSession(r.Context(), orgID, user.ID, sid)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load session")
		return
	}
	msgs, err := s.Chat.ListMessages(r.Context(), sess.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not load messages")
		return
	}
	out := make([]chatThreadMsg, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, chatThreadMsg{
			ID: m.ID, Role: m.Role, Content: m.Content,
			InputTokens: m.InputTokens, OutputTokens: m.OutputTokens,
			ToolCalls: m.ToolCalls, CreatedAt: m.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          sess.ID,
		"title":       sess.Title,
		"totalTokens": sess.TotalTokens,
		"messages":    out,
	})
}

func (s *Server) handleDeleteChatSession(w http.ResponseWriter, r *http.Request) {
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	sid, err := uuid.Parse(chi.URLParam(r, "sid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid session id")
		return
	}
	if err := s.Chat.DeleteSession(r.Context(), orgID, user.ID, sid); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type chatPostReq struct {
	Text string `json:"text"`
}

type chatPostResp struct {
	SessionID    uuid.UUID            `json:"sessionId"`
	Assistant    string               `json:"assistant"`
	ToolCalls    []chat.ToolCallTrace `json:"toolCalls"`
	InputTokens  int                  `json:"inputTokens"`
	OutputTokens int                  `json:"outputTokens"`
	Usage        chat.BudgetStatus    `json:"usage"`
	Title        string               `json:"title"`
}

func (s *Server) handlePostChatMessage(w http.ResponseWriter, r *http.Request) {
	if s.ChatClient == nil || s.ChatDispatcher == nil {
		writeError(w, http.StatusServiceUnavailable, "chat_unavailable", "chat is not configured on this deployment")
		return
	}
	orgID := orgFromCtx(r.Context())
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	sid, err := uuid.Parse(chi.URLParam(r, "sid"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid session id")
		return
	}
	var req chatPostReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "bad json")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "text required")
		return
	}
	sess, err := s.Chat.GetSession(r.Context(), orgID, user.ID, sid)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "session lookup failed")
		return
	}

	now := time.Now().UTC()
	status, err := chat.CheckBudget(r.Context(), s.Chat, orgID, s.Cfg.ChatDailyTokenBudget, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "budget check failed")
		return
	}
	if status.OverBudget {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":   "daily token budget exceeded",
			"resetAt": status.ResetAt,
			"used":    status.Used,
			"budget":  status.Budget,
		})
		return
	}

	// Rebuild prior conversation for the model. Stored content is the raw
	// Parts array per turn (Gemini Content.Parts shape).
	priorMsgs, err := s.Chat.ListMessages(r.Context(), sess.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load history failed")
		return
	}
	prior := make([]chat.Content, 0, len(priorMsgs))
	for _, m := range priorMsgs {
		var parts []chat.Part
		if err := json.Unmarshal(m.Content, &parts); err != nil {
			// Skip malformed rows rather than failing the whole turn.
			continue
		}
		// Gemini wants "user"/"model"; persisted rows use "user"/"assistant"
		// for the SPA. Translate at the boundary.
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		prior = append(prior, chat.Content{Role: role, Parts: parts})
	}

	res, runErr := chat.RunLoop(
		r.Context(),
		s.ChatClient.Send,
		s.ChatDispatcher,
		orgID,
		s.Cfg.ChatModel,
		prior,
		req.Text,
		s.Cfg.ChatMaxIterations,
	)
	if runErr != nil && res == nil {
		log.Printf("chat: loop: %v", runErr)
		writeError(w, http.StatusBadGateway, "chat_upstream", "chat upstream failed")
		return
	}

	// Persist every new message produced this turn. Token usage is attached
	// to the last assistant turn; user/tool-result turns store 0.
	for i, m := range res.ConversationDelta {
		raw, err := json.Marshal(m.Parts)
		if err != nil {
			continue
		}
		role := m.Role
		// Translate Gemini's "model" back to "assistant" for SPA consumption.
		if role == "model" {
			role = "assistant"
		}
		am := store.AppendMessage{
			Role:    role,
			Content: raw,
		}
		if role == "assistant" && i == len(res.ConversationDelta)-1 {
			am.InputTokens = res.InputTokens
			am.OutputTokens = res.OutputTokens
			if len(res.ToolCalls) > 0 {
				tc, _ := json.Marshal(res.ToolCalls)
				am.ToolCalls = tc
			}
		}
		if _, err := s.Chat.AppendMessage(r.Context(), sess.ID, am); err != nil {
			log.Printf("chat: append message: %v", err)
		}
	}

	// Auto-title from the first user message if not already set.
	title := sess.Title
	if title == "" {
		title = truncate(req.Text, 60)
		if err := s.Chat.SetTitle(r.Context(), sess.ID, title); err != nil {
			log.Printf("chat: set title: %v", err)
		}
	}

	if err := chat.RecordUsage(r.Context(), s.Chat, orgID, now, res.InputTokens, res.OutputTokens); err != nil {
		log.Printf("chat: record usage: %v", err)
	}

	// Best-effort audit line. Goes to stdout (Cloud Run captures these) since
	// there is no per-org audit table yet; tracked as a follow-up.
	log.Printf("chat: org=%s user=%s session=%s tools=%d input=%d output=%d",
		orgID, user.ID, sess.ID, len(res.ToolCalls), res.InputTokens, res.OutputTokens)

	newStatus, _ := chat.CheckBudget(r.Context(), s.Chat, orgID, s.Cfg.ChatDailyTokenBudget, now)
	writeJSON(w, http.StatusOK, chatPostResp{
		SessionID:    sess.ID,
		Assistant:    res.AssistantText,
		ToolCalls:    res.ToolCalls,
		InputTokens:  res.InputTokens,
		OutputTokens: res.OutputTokens,
		Usage:        newStatus,
		Title:        title,
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}
