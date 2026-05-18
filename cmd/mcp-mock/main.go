// Command mcp-mock is a tiny stand-in for a real MCP (Model Context
// Protocol) server. It exists so the live demo can show AgentShield's
// per-tool circuit breaker tripping when "the MCP server starts erroring
// out" — one of the three failure modes the TrueFoundry Resilient
// Agents Challenge brief explicitly names.
//
// We don't implement the full MCP spec here; this is a wire-compatible
// stub for the demo's mcp_lookup ReAct tool. Production users point
// MCP_URL at a real MCP server (Anthropic's reference impl, fastmcp,
// any server speaking the MCP HTTP transport) and the resilience
// guarantees are identical because the circuit breaker wraps the HTTP
// boundary, not anything spec-specific.
//
// Endpoints:
//
//	POST /mcp/call     {"tool":"weather","args":"Berlin"}
//	                   200 OK {"result":"..."} | 503 if killed
//	POST /mcp/kill     start returning 503 on /mcp/call
//	POST /mcp/restore  resume serving /mcp/call
//	GET  /mcp/status   {"killed":bool,"tools":["weather","currency","fact"]}
//	GET  /health       process liveness for k8s/docker
//
// Run:  go run ./cmd/mcp-mock              (defaults to :8081)
//       MCP_MOCK_PORT=9090 go run ./cmd/mcp-mock
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	port := os.Getenv("MCP_MOCK_PORT")
	if port == "" {
		port = "8081"
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	var killed atomic.Bool

	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("POST /mcp/kill", func(w http.ResponseWriter, r *http.Request) {
		killed.Store(true)
		log.Warn("mcp-mock: killed (subsequent /mcp/call → 503)")
		writeJSON(w, http.StatusOK, map[string]string{"status": "killed"})
	})

	mux.HandleFunc("POST /mcp/restore", func(w http.ResponseWriter, r *http.Request) {
		killed.Store(false)
		log.Info("mcp-mock: restored")
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /mcp/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"killed": killed.Load(),
			"tools":  []string{"weather", "currency", "fact"},
		})
	})

	mux.HandleFunc("POST /mcp/call", func(w http.ResponseWriter, r *http.Request) {
		if killed.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "mcp server unavailable (chaos demo)",
			})
			return
		}
		var req struct {
			Tool string `json:"tool"`
			Args string `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
			return
		}
		// Hand back canned responses for the three demo tools. Real MCP
		// servers do real things here; for the demo, the value is in
		// showing the CB protect the call site, not in the responses.
		result := handleCall(req.Tool, req.Args)
		writeJSON(w, http.StatusOK, map[string]string{"result": result})
	})

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Info("mcp-mock listening", "addr", "http://localhost:"+port,
		"endpoints", "GET /health, GET /mcp/status, POST /mcp/{call,kill,restore}")
	if err := srv.ListenAndServe(); err != nil {
		log.Error("server error", "err", err)
		os.Exit(1)
	}
}

func handleCall(tool, args string) string {
	switch strings.ToLower(tool) {
	case "weather":
		city := args
		if city == "" {
			city = "(unspecified)"
		}
		return fmt.Sprintf("Current weather in %s: 18°C, partly cloudy. (mock data)", city)
	case "currency":
		return fmt.Sprintf("1 USD = 0.92 EUR (mock rate for %q)", args)
	case "fact":
		return "Mock fact: Go was released in November 2009 at Google."
	default:
		return fmt.Sprintf("Mock MCP server received tool=%q args=%q (no canned response).", tool, args)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
