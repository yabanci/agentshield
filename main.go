package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yabanci/agentshield/agent"
	"github.com/yabanci/agentshield/api"
	"github.com/yabanci/agentshield/config"
)

func main() {
	// Bootstrap logger uses defaults; once config is loaded, we rebuild from cfg.
	bootstrap := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.LoadFromEnv()
	if err != nil {
		bootstrap.Error("config load failed", "err", err)
		os.Exit(1)
	}

	logger := cfg.NewLogger(os.Stdout)
	slog.SetDefault(logger)

	a := agent.NewWithConfig(cfg, logger)
	defer a.Stop()
	h := api.New(a, cfg)

	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  70 * time.Second,
		WriteTimeout: 70 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if cfg.AuthToken == "" {
		logger.Warn("AGENTSHIELD_AUTH_TOKEN is unset — /demo/*, /sessions/*, " +
			"/trace/*, and /config/webhook are open to anonymous callers. " +
			"Required in any deployment a third party can reach. " +
			"(Kill/degrade actions auto-restore after 5 minutes regardless.)")
	}

	// Discoverability warning for the OpenAI-only case: if the chat backend
	// is OpenAI but no OpenAI embedding model is set, every embedding will
	// route to Ollama at localhost. Judges deploying purely against OpenAI
	// previously saw a silent semantic-cache failure with no explanation.
	if cfg.Provider.Kind == "openai" && cfg.Provider.EmbedModel == "" {
		logger.Warn("LLM_PROVIDER=openai but OPENAI_EMBED_MODEL is unset. " +
			"Embeddings will require Ollama reachable at " +
			"http://localhost:11434. Set OPENAI_EMBED_MODEL=text-embedding-3-small " +
			"to route embeddings through OpenAI instead.")
	}

	go func() {
		logger.Info("AgentShield started", "addr", "http://localhost:"+cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "err", err)
	}
	logger.Info("AgentShield stopped")
}
