package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yabanci/agentshield/agent"
)

type chatRequest struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// Handler holds all HTTP route handlers.
type Handler struct {
	agent *agent.Agent
}

func New(a *agent.Agent) *Handler {
	return &Handler{agent: a}
}

func (h *Handler) Register(mux *http.ServeMux) {
	// Core chat
	mux.HandleFunc("POST /chat", h.chat)
	mux.HandleFunc("GET /chat/stream", h.chatStream)

	// ReAct agent
	mux.HandleFunc("POST /react", h.react)

	// Sessions
	mux.HandleFunc("GET /sessions", h.listSessions)
	mux.HandleFunc("GET /sessions/{id}", h.getSession)
	mux.HandleFunc("DELETE /sessions/{id}", h.deleteSession)

	// Status & metrics
	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("GET /health", h.health)
	mux.Handle("GET /metrics", promhttp.Handler())

	// Demo controls
	mux.HandleFunc("POST /demo/kill", h.killPrimary)
	mux.HandleFunc("POST /demo/restore", h.restorePrimary)
	mux.HandleFunc("POST /demo/kill-fallback", h.killFallback)
	mux.HandleFunc("POST /demo/restore-fallback", h.restoreFallback)
	mux.HandleFunc("POST /demo/chaos", h.startChaos)
	mux.HandleFunc("GET /demo/chaos/stream", h.chaosStream)

	// Dashboard
	mux.HandleFunc("GET /", h.dashboard)
}

// ─── Chat ──────────────────────────────────────────────────────────────────

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		jsonError(w, "prompt is required", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	batch := r.Header.Get("X-Priority") == "batch"
	var (
		resp agent.Response
		err  error
	)
	if batch {
		resp, err = h.agent.AskBatch(ctx, req.Prompt)
	} else {
		resp, err = h.agent.Ask(ctx, req.Prompt)
	}
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, resp)
}

func (h *Handler) chatStream(w http.ResponseWriter, r *http.Request) {
	prompt := r.URL.Query().Get("prompt")
	if prompt == "" {
		http.Error(w, "prompt query param required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	tokens := make(chan string, 64)
	var tier agent.Tier

	go func() {
		defer close(tokens)
		tier, _ = h.agent.StreamPrimary(ctx, prompt, tokens)
	}()

	for token := range tokens {
		data, _ := json.Marshal(map[string]any{"token": token, "done": false})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	done, _ := json.Marshal(map[string]any{"done": true, "tier": string(tier)})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", done)
	flusher.Flush()
}

// ─── ReAct ─────────────────────────────────────────────────────────────────

func (h *Handler) react(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		jsonError(w, "prompt is required", http.StatusBadRequest)
		return
	}

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = r.Header.Get("X-Session-ID")
	}
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	resp, err := h.agent.React(ctx, req.Prompt, sessionID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, resp)
}

// ─── Sessions ──────────────────────────────────────────────────────────────

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, h.agent.ListSessions())
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := h.agent.GetSession(id)
	if sess == nil {
		jsonError(w, "session not found", http.StatusNotFound)
		return
	}
	jsonOK(w, sess)
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h.agent.DeleteSession(id)
	jsonOK(w, map[string]string{"result": "deleted"})
}

// ─── Status ────────────────────────────────────────────────────────────────

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, h.agent.Status())
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := h.agent.Ping(ctx); err != nil {
		jsonError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

// ─── Demo controls ─────────────────────────────────────────────────────────

func (h *Handler) killPrimary(w http.ResponseWriter, r *http.Request) {
	h.agent.KillPrimary()
	jsonOK(w, map[string]string{"result": "primary model killed"})
}

func (h *Handler) restorePrimary(w http.ResponseWriter, r *http.Request) {
	h.agent.RestorePrimary()
	jsonOK(w, map[string]string{"result": "primary model restored"})
}

func (h *Handler) killFallback(w http.ResponseWriter, r *http.Request) {
	h.agent.KillFallback()
	jsonOK(w, map[string]string{"result": "fallback model killed"})
}

func (h *Handler) restoreFallback(w http.ResponseWriter, r *http.Request) {
	h.agent.RestoreFallback()
	jsonOK(w, map[string]string{"result": "fallback model restored"})
}

func (h *Handler) startChaos(w http.ResponseWriter, r *http.Request) {
	// Non-blocking: return 202, chaos runs in background
	ctx := context.Background() // independent of request lifetime
	_, err := h.agent.StartChaos(ctx)
	if err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	jsonOK(w, map[string]string{"result": "chaos scenario started — stream at /demo/chaos/stream"})
}

// chaosStream streams chaos events via SSE.
// The client connects, starts chaos via POST /demo/chaos, then watches here.
func (h *Handler) chaosStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	ch, err := h.agent.StartChaos(ctx)
	if err != nil {
		jsonError(w, err.Error(), http.StatusConflict)
		return
	}

	for event := range ch {
		data, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		if event.Type == "done" {
			return
		}
	}
}

// ─── Dashboard ─────────────────────────────────────────────────────────────

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: msg})
}

func generateSessionID() string {
	return fmt.Sprintf("s%d", time.Now().UnixNano())
}
