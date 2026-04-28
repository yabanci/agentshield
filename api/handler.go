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
	Prompt string `json:"prompt"`
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
	mux.HandleFunc("POST /chat", h.chat)
	mux.HandleFunc("GET /chat/stream", h.chatStream)
	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("POST /demo/kill", h.killPrimary)
	mux.HandleFunc("POST /demo/restore", h.restorePrimary)
	mux.HandleFunc("POST /demo/kill-fallback", h.killFallback)
	mux.HandleFunc("POST /demo/restore-fallback", h.restoreFallback)
	mux.HandleFunc("GET /health", h.health)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /", h.dashboard)
}

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

// chatStream streams tokens via Server-Sent Events.
// Usage: GET /chat/stream?prompt=your+question
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
	var streamErr error
	var tier agent.Tier

	go func() {
		defer close(tokens)
		tier, streamErr = h.agent.StreamPrimary(ctx, prompt, tokens)
		_ = streamErr
	}()

	for token := range tokens {
		data, _ := json.Marshal(map[string]any{"token": token, "done": false})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Send final event with tier info
	done, _ := json.Marshal(map[string]any{"done": true, "tier": string(tier)})
	fmt.Fprintf(w, "data: %s\n\n", done)
	flusher.Flush()
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, h.agent.Status())
}

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

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.agent.Ping(ctx); err != nil {
		jsonError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	jsonOK(w, map[string]string{"status": "ok"})
}

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(errorResponse{Error: msg})
}
