package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

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
	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("POST /demo/kill", h.killPrimary)
	mux.HandleFunc("POST /demo/restore", h.restorePrimary)
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /", h.dashboard)
}

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		jsonError(w, "prompt is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	resp, err := h.agent.Ask(ctx, req.Prompt)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonOK(w, resp)
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
