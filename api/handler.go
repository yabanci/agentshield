package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/yabanci/agentshield/agent"
	"github.com/yabanci/agentshield/config"
)

// MaxPromptBytes caps incoming prompts to prevent memory blow-up from
// adversarial or buggy clients (default: 32 KiB).
const MaxPromptBytes = 32 * 1024

type chatRequest struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id,omitempty"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// validatePrompt rejects empty or oversized prompts.
func validatePrompt(prompt string) (int, string, bool) {
	if prompt == "" {
		return http.StatusBadRequest, "prompt is required", false
	}
	if len(prompt) > MaxPromptBytes {
		return http.StatusRequestEntityTooLarge,
			fmt.Sprintf("prompt exceeds maximum size of %d bytes", MaxPromptBytes), false
	}
	return 0, "", true
}

// Handler holds all HTTP route handlers.
type Handler struct {
	agent     *agent.Agent
	cfg       *config.Config
	ipLimiter *ipLimiter
}

func New(a *agent.Agent, cfg *config.Config) *Handler {
	return &Handler{agent: a, cfg: cfg, ipLimiter: newIPLimiter()}
}

func (h *Handler) Register(mux *http.ServeMux) {
	// Core chat — IP rate-limited (60 req/min/IP) to prevent saturation
	mux.HandleFunc("POST /chat", h.ipLimiter.middleware(h.chat))
	mux.HandleFunc("GET /chat/stream", h.ipLimiter.middleware(h.chatStream))

	// ReAct agent — also rate-limited
	mux.HandleFunc("POST /react", h.ipLimiter.middleware(h.react))

	// Sessions
	mux.HandleFunc("GET /sessions", h.listSessions)
	mux.HandleFunc("GET /sessions/{id}", h.getSession)
	mux.HandleFunc("DELETE /sessions/{id}", h.deleteSession)

	// Status & metrics
	mux.HandleFunc("GET /status", h.status)
	mux.HandleFunc("GET /score/history", h.scoreHistory)
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /health/live", h.healthLive)
	mux.HandleFunc("GET /health/ready", h.healthReady)
	mux.Handle("GET /metrics", promhttp.Handler())

	// Demo controls (auth-gated when AGENTSHIELD_AUTH_TOKEN is set)
	mux.HandleFunc("POST /demo/kill", h.requireAuth(h.killPrimary))
	mux.HandleFunc("POST /demo/restore", h.requireAuth(h.restorePrimary))
	mux.HandleFunc("POST /demo/kill-fallback", h.requireAuth(h.killFallback))
	mux.HandleFunc("POST /demo/restore-fallback", h.requireAuth(h.restoreFallback))
	mux.HandleFunc("POST /demo/chaos", h.requireAuth(h.startChaos))
	mux.HandleFunc("GET /demo/chaos/stream", h.requireAuth(h.chaosStream))
	mux.HandleFunc("POST /demo/degrade", h.requireAuth(h.enableDegrade))
	mux.HandleFunc("POST /demo/restore-quality", h.requireAuth(h.disableDegrade))

	// Auth discovery for the dashboard
	mux.HandleFunc("GET /auth/required", h.authRequired)

	// Trace
	mux.HandleFunc("GET /trace/{id}", h.getTrace)

	// Webhook config (auth-gated)
	mux.HandleFunc("POST /config/webhook", h.requireAuth(h.setWebhook))
	mux.HandleFunc("GET /config/webhook", h.getWebhook)
	mux.HandleFunc("DELETE /config/webhook", h.requireAuth(h.clearWebhook))

	// Dashboard
	mux.HandleFunc("GET /", h.dashboard)
}

// ─── Chat ──────────────────────────────────────────────────────────────────

func (h *Handler) chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, MaxPromptBytes+1024)).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if code, msg, ok := validatePrompt(req.Prompt); !ok {
		jsonError(w, msg, code)
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
	if code, msg, ok := validatePrompt(prompt); !ok {
		http.Error(w, msg, code)
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

	out := make(chan agent.StreamToken, 64)
	var tier agent.Tier

	go func() {
		defer close(out)
		tier, _ = h.agent.StreamWithQualityGate(ctx, prompt, out)
	}()

	// SSE heartbeat: send a comment every 15s if no token has been sent,
	// to keep proxies/load balancers from killing the connection.
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case st, ok := <-out:
			if !ok {
				done, _ := json.Marshal(agent.StreamToken{Done: true, Tier: tier})
				_, _ = fmt.Fprintf(w, "data: %s\n\n", done)
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(st)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			// Reset heartbeat: real activity counts as a keepalive.
			heartbeat.Reset(15 * time.Second)
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ":heartbeat\n\n")
			flusher.Flush()
		case <-ctx.Done():
			return
		}
	}
}

// ─── ReAct ─────────────────────────────────────────────────────────────────

func (h *Handler) react(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, MaxPromptBytes+1024)).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if code, msg, ok := validatePrompt(req.Prompt); !ok {
		jsonError(w, msg, code)
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

func (h *Handler) scoreHistory(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, h.agent.ScoreHistorySnapshot())
}

// health is the legacy combined endpoint (alias for /health/ready).
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	h.healthReady(w, r)
}

// healthLive is the Kubernetes liveness probe — returns 200 if the
// process is alive. Does NOT check dependencies, so a brief Ollama outage
// won't trigger pod restart.
func (h *Handler) healthLive(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]string{"status": "alive"})
}

// healthReady is the Kubernetes readiness probe — returns 200 only if
// dependencies (Ollama) are reachable. K8s removes the pod from the
// service if this fails, but won't restart it.
func (h *Handler) healthReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := h.agent.Ping(ctx); err != nil {
		jsonError(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	jsonOK(w, map[string]string{"status": "ready"})
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

func (h *Handler) enableDegrade(w http.ResponseWriter, r *http.Request) {
	h.agent.EnableDegradeMode()
	jsonOK(w, map[string]string{"result": "degrade mode ON — primary returns low-quality responses"})
}

func (h *Handler) disableDegrade(w http.ResponseWriter, r *http.Request) {
	h.agent.DisableDegradeMode()
	jsonOK(w, map[string]string{"result": "degrade mode OFF — primary restored"})
}

func (h *Handler) startChaos(w http.ResponseWriter, r *http.Request) {
	// Non-blocking: return 202, chaos runs in background.
	// Use the agent's lifetime context so chaos terminates on server shutdown
	// rather than running forever (would block clean exit).
	_, err := h.agent.StartChaos(h.agent.LifecycleContext())
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

func (h *Handler) getTrace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tr := h.agent.GetTrace(id)
	if tr == nil {
		jsonError(w, "trace not found", http.StatusNotFound)
		return
	}
	jsonOK(w, tr)
}

func (h *Handler) setWebhook(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		jsonError(w, "url is required", http.StatusBadRequest)
		return
	}
	if err := validateWebhookURL(body.URL, h.cfg); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.agent.SetWebhookURL(body.URL)
	jsonOK(w, map[string]string{"result": "webhook configured", "url": body.URL})
}

func (h *Handler) getWebhook(w http.ResponseWriter, r *http.Request) {
	url := h.agent.WebhookURL()
	if url == "" {
		jsonOK(w, map[string]any{"configured": false})
		return
	}
	jsonOK(w, map[string]any{"configured": true, "url": url})
}

// authRequired tells the dashboard whether bearer-token auth is enabled.
func (h *Handler) authRequired(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]bool{"required": h.AuthEnabled()})
}

func (h *Handler) clearWebhook(w http.ResponseWriter, r *http.Request) {
	h.agent.ClearWebhookURL()
	jsonOK(w, map[string]string{"result": "webhook cleared"})
}

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
