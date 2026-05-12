# AgentShield Foundation Refactor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `agent.Agent` (27-field God-object) into a 7-field facade over 7 well-bounded packages (`config`, `provider`, `quality`, `cache`, `memory`, `telemetry`, `orchestrator`), extract the dashboard from a Go string literal into `embed.FS`, and introduce structured `slog` logging — without changing observable behaviour.

**Architecture:** Strangler-pattern across six sequential MRs (F1 → F2 → F3a → F3b → F4 → F5). Each MR keeps `go test -race ./...` and CI green by itself. Tests migrate with their source. `agent.New()` (no-arg) preserved as backward-compatible convenience.

**Tech Stack:** Go 1.24, flowguard v0.2.0, prometheus client v1.23, slog (stdlib), html/template + embed.FS, golangci-lint v2.11.4. No new runtime deps beyond what already exists.

**Spec:** `docs/superpowers/specs/2026-05-12-foundation-refactor-design.md`

---

## Conventions

- All commands assume cwd = repo root.
- All commits follow conventional-commits (`feat:`, `refactor:`, `test:`, `docs:`, `chore:`).
- Branch per MR: `feat/foundation-f1-config`, `feat/foundation-f2-provider`, etc.
- After each MR is complete: PR to `main`, wait for CI green, merge.
- Every MR ends with three verification commands run together:
  ```bash
  go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
  ```
  All three must succeed before commit. If any fails, stop and fix root cause — do not proceed.

---

# PHASE F1 — `config` package

**Branch:** `feat/foundation-f1-config`
**Outcome:** Single typed `*config.Config`. Zero `os.Getenv` outside `config/`. `agent.New()` still works (loads env internally).

## Task F1.0: Create branch

- [ ] **Step 1: Create and checkout branch**

```bash
git checkout -b feat/foundation-f1-config
```

## Task F1.1: Skeleton config package + LoggerConfig

**Files:**
- Create: `config/config.go`
- Create: `config/config_test.go`

- [ ] **Step 1: Write failing test for LoggerConfig defaults**

`config/config_test.go`:
```go
package config

import (
	"log/slog"
	"testing"
)

func TestDefaults_Logger(t *testing.T) {
	c := Defaults()
	if c.Logger.Level != slog.LevelInfo {
		t.Errorf("Logger.Level = %v, want INFO", c.Logger.Level)
	}
	if c.Logger.Format != "text" {
		t.Errorf("Logger.Format = %q, want text", c.Logger.Format)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./config/... -run TestDefaults_Logger -v
```
Expected: build error — package `config` does not exist.

- [ ] **Step 3: Create config.go with skeleton + Defaults()**

`config/config.go`:
```go
// Package config holds AgentShield's typed runtime configuration.
// Every os.Getenv read in the project lives here; no other package may import "os"
// for environment access.
package config

import (
	"log/slog"
	"time"
)

type Config struct {
	Port      string
	AuthToken string
	Logger    LoggerConfig
}

type LoggerConfig struct {
	Level  slog.Level
	Format string // "text" | "json"
}

// Defaults returns a Config populated with safe production defaults.
// Validate is NOT called here — caller must invoke Validate after merging env.
func Defaults() *Config {
	return &Config{
		Port: "8080",
		Logger: LoggerConfig{
			Level:  slog.LevelInfo,
			Format: "text",
		},
	}
}

// Suppress unused-import warnings until later tasks reference time.Duration fields.
var _ = time.Second
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./config/... -run TestDefaults_Logger -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat(config): skeleton package with LoggerConfig defaults"
```

## Task F1.2: ProviderConfig + ModelsConfig defaults

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`

- [ ] **Step 1: Add failing test for Provider+Models defaults**

Append to `config/config_test.go`:
```go
func TestDefaults_ProviderAndModels(t *testing.T) {
	c := Defaults()
	if c.Provider.Kind != "ollama" {
		t.Errorf("Provider.Kind = %q, want ollama", c.Provider.Kind)
	}
	if c.Provider.BaseURL != "http://localhost:11434" {
		t.Errorf("Provider.BaseURL = %q, want http://localhost:11434", c.Provider.BaseURL)
	}
	if c.Provider.Timeout != 60*time.Second {
		t.Errorf("Provider.Timeout = %v, want 60s", c.Provider.Timeout)
	}
	if c.Models.Primary != "llama3.2" {
		t.Errorf("Models.Primary = %q, want llama3.2", c.Models.Primary)
	}
	if c.Models.Fallback != "llama3.2:1b" {
		t.Errorf("Models.Fallback = %q, want llama3.2:1b", c.Models.Fallback)
	}
	if c.Models.Embedding != "nomic-embed-text" {
		t.Errorf("Models.Embedding = %q, want nomic-embed-text", c.Models.Embedding)
	}
}
```

Add `import "time"` to test file.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./config/... -run TestDefaults_ProviderAndModels -v
```
Expected: FAIL — fields don't exist.

- [ ] **Step 3: Add ProviderConfig + ModelsConfig + defaults**

In `config/config.go` add to `Config` struct and define types:

```go
// In Config:
//   Provider  ProviderConfig
//   Models    ModelsConfig

type ProviderConfig struct {
	Kind    string
	BaseURL string
	Timeout time.Duration
}

type ModelsConfig struct {
	Primary   string
	Fallback  string
	Embedding string
}
```

Update `Defaults()`:
```go
return &Config{
	Port: "8080",
	Logger: LoggerConfig{Level: slog.LevelInfo, Format: "text"},
	Provider: ProviderConfig{
		Kind:    "ollama",
		BaseURL: "http://localhost:11434",
		Timeout: 60 * time.Second,
	},
	Models: ModelsConfig{
		Primary:   "llama3.2",
		Fallback:  "llama3.2:1b",
		Embedding: "nomic-embed-text",
	},
}
```

Remove the `var _ = time.Second` placeholder.

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./config/... -v
```
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat(config): add Provider and Models defaults"
```

## Task F1.3: LimitsConfig (12 fields)

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`

- [ ] **Step 1: Add failing test**

Append to `config/config_test.go`:
```go
func TestDefaults_Limits(t *testing.T) {
	c := Defaults()
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"MaxPromptBytes", c.Limits.MaxPromptBytes, 32 * 1024},
		{"ToolTimeout", c.Limits.ToolTimeout, 10 * time.Second},
		{"InteractiveSlots", c.Limits.InteractiveSlots, 20},
		{"BatchSlots", c.Limits.BatchSlots, 5},
		{"LoadshedStart", c.Limits.LoadshedStart, 50},
		{"LoadshedWindow", c.Limits.LoadshedWindow, 5 * time.Second},
		{"PrimaryCBWindow", c.Limits.PrimaryCBWindow, 20},
		{"PrimaryCBErrorRate", c.Limits.PrimaryCBErrorRate, 0.5},
		{"FallbackCBThreshold", c.Limits.FallbackCBThreshold, 3},
		{"HedgeDelay", c.Limits.HedgeDelay, 1500 * time.Millisecond},
		{"RetryMax", c.Limits.RetryMax, 2},
		{"RetryBaseBackoff", c.Limits.RetryBaseBackoff, 300 * time.Millisecond},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./config/... -run TestDefaults_Limits -v
```
Expected: build error or FAIL.

- [ ] **Step 3: Implement LimitsConfig**

In `config/config.go`, add to Config struct field `Limits LimitsConfig` and type:
```go
type LimitsConfig struct {
	MaxPromptBytes      int
	ToolTimeout         time.Duration
	InteractiveSlots    int
	BatchSlots          int
	LoadshedStart       int
	LoadshedWindow      time.Duration
	PrimaryCBWindow     int
	PrimaryCBErrorRate  float64
	FallbackCBThreshold int
	HedgeDelay          time.Duration
	RetryMax            int
	RetryBaseBackoff    time.Duration
}
```

In `Defaults()` add:
```go
Limits: LimitsConfig{
	MaxPromptBytes:      32 * 1024,
	ToolTimeout:         10 * time.Second,
	InteractiveSlots:    20,
	BatchSlots:          5,
	LoadshedStart:       50,
	LoadshedWindow:      5 * time.Second,
	PrimaryCBWindow:     20,
	PrimaryCBErrorRate:  0.5,
	FallbackCBThreshold: 3,
	HedgeDelay:          1500 * time.Millisecond,
	RetryMax:            2,
	RetryBaseBackoff:    300 * time.Millisecond,
},
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./config/... -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat(config): add Limits defaults (CB, bulkhead, loadshed, hedge, retry)"
```

## Task F1.4: Quality + Cache + Webhook + Score configs

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`

- [ ] **Step 1: Add failing test**

Append to `config/config_test.go`:
```go
func TestDefaults_QualityCacheWebhookScore(t *testing.T) {
	c := Defaults()
	if c.Quality.AcceptableScore != 0.45 {
		t.Errorf("Quality.AcceptableScore = %v, want 0.45", c.Quality.AcceptableScore)
	}
	if c.Quality.DriftWindow != 50 {
		t.Errorf("Quality.DriftWindow = %d, want 50", c.Quality.DriftWindow)
	}
	if c.Quality.DriftSigma != 2.0 {
		t.Errorf("Quality.DriftSigma = %v, want 2.0", c.Quality.DriftSigma)
	}
	if c.Cache.TTL != 10*time.Minute {
		t.Errorf("Cache.TTL = %v, want 10m", c.Cache.TTL)
	}
	if c.Cache.SimilarityThreshold != 0.92 {
		t.Errorf("Cache.SimilarityThreshold = %v, want 0.92", c.Cache.SimilarityThreshold)
	}
	if c.Cache.MaxEntries != 1024 {
		t.Errorf("Cache.MaxEntries = %d, want 1024", c.Cache.MaxEntries)
	}
	if !c.Cache.EmbedAsync {
		t.Errorf("Cache.EmbedAsync = false, want true")
	}
	if c.Webhook.Timeout != 5*time.Second {
		t.Errorf("Webhook.Timeout = %v, want 5s", c.Webhook.Timeout)
	}
	if c.Webhook.AllowHTTP {
		t.Errorf("Webhook.AllowHTTP = true, want false")
	}
	if c.Webhook.AllowPrivate {
		t.Errorf("Webhook.AllowPrivate = true, want false")
	}
	if c.Score.HistorySize != 60 {
		t.Errorf("Score.HistorySize = %d, want 60", c.Score.HistorySize)
	}
	if c.Score.LatencyP95Target != 3*time.Second {
		t.Errorf("Score.LatencyP95Target = %v, want 3s", c.Score.LatencyP95Target)
	}
	wantWeights := map[string]int{"transport": 20, "quality": 20, "cache": 20, "availability": 20, "latency": 20}
	for k, v := range wantWeights {
		if c.Score.Weights[k] != v {
			t.Errorf("Score.Weights[%q] = %d, want %d", k, c.Score.Weights[k], v)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./config/... -run TestDefaults_QualityCacheWebhookScore -v
```
Expected: build error.

- [ ] **Step 3: Implement remaining configs**

Add to Config struct:
```go
	Quality   QualityConfig
	Cache     CacheConfig
	Webhook   WebhookConfig
	Score     ScoreConfig
```

Add types:
```go
type QualityConfig struct {
	AcceptableScore float64
	DriftWindow     int
	DriftSigma      float64
}

type CacheConfig struct {
	TTL                 time.Duration
	SimilarityThreshold float64
	MaxEntries          int
	EmbedAsync          bool
}

type WebhookConfig struct {
	AllowHTTP    bool
	AllowPrivate bool
	Timeout      time.Duration
}

type ScoreConfig struct {
	HistorySize      int
	LatencyP95Target time.Duration
	Weights          map[string]int
}
```

In `Defaults()` add:
```go
Quality: QualityConfig{AcceptableScore: 0.45, DriftWindow: 50, DriftSigma: 2.0},
Cache:   CacheConfig{TTL: 10 * time.Minute, SimilarityThreshold: 0.92, MaxEntries: 1024, EmbedAsync: true},
Webhook: WebhookConfig{Timeout: 5 * time.Second},
Score: ScoreConfig{
	HistorySize:      60,
	LatencyP95Target: 3 * time.Second,
	Weights: map[string]int{
		"transport": 20, "quality": 20, "cache": 20, "availability": 20, "latency": 20,
	},
},
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./config/... -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat(config): add Quality/Cache/Webhook/Score defaults"
```

## Task F1.5: LoadFromEnv

**Files:**
- Create: `config/env.go`
- Modify: `config/config_test.go`

- [ ] **Step 1: Add failing test**

Append to `config/config_test.go`:
```go
func TestLoadFromEnv_Overrides(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("OLLAMA_URL", "http://ollama.example:11434")
	t.Setenv("AGENTSHIELD_AUTH_TOKEN", "secret-xyz")
	t.Setenv("AGENTSHIELD_ALLOW_HTTP_WEBHOOK", "true")
	t.Setenv("AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK", "true")

	c, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if c.Port != "9090" {
		t.Errorf("Port = %q, want 9090", c.Port)
	}
	if c.Provider.BaseURL != "http://ollama.example:11434" {
		t.Errorf("Provider.BaseURL = %q, want http://ollama.example:11434", c.Provider.BaseURL)
	}
	if c.AuthToken != "secret-xyz" {
		t.Errorf("AuthToken = %q, want secret-xyz", c.AuthToken)
	}
	if !c.Webhook.AllowHTTP {
		t.Errorf("Webhook.AllowHTTP = false, want true")
	}
	if !c.Webhook.AllowPrivate {
		t.Errorf("Webhook.AllowPrivate = false, want true")
	}
}

func TestLoadFromEnv_NoOverridesUsesDefaults(t *testing.T) {
	// t.Setenv with empty string still sets the var; clear instead.
	t.Setenv("PORT", "")
	c, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("LoadFromEnv: %v", err)
	}
	if c.Port != "8080" {
		t.Errorf("Port = %q, want default 8080", c.Port)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./config/... -run TestLoadFromEnv -v
```
Expected: build error — `LoadFromEnv` not defined.

- [ ] **Step 3: Implement LoadFromEnv**

`config/env.go`:
```go
package config

import (
	"fmt"
	"os"
)

// LoadFromEnv builds a Config from defaults, overrides with environment variables,
// then validates. Returns the first validation error if any.
func LoadFromEnv() (*Config, error) {
	c := Defaults()

	if v := os.Getenv("PORT"); v != "" {
		c.Port = v
	}
	if v := os.Getenv("OLLAMA_URL"); v != "" {
		c.Provider.BaseURL = v
	}
	if v := os.Getenv("AGENTSHIELD_AUTH_TOKEN"); v != "" {
		c.AuthToken = v
	}
	if os.Getenv("AGENTSHIELD_ALLOW_HTTP_WEBHOOK") == "true" {
		c.Webhook.AllowHTTP = true
	}
	if os.Getenv("AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK") == "true" {
		c.Webhook.AllowPrivate = true
	}

	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return c, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./config/... -v
```
Expected: PASS (depends on `Validate` being a no-op stub — add it now).

In `config/config.go` add stub:
```go
// Validate returns nil for now; F1.6 fills in real checks.
func (c *Config) Validate() error { return nil }
```

Re-run test:
```bash
go test ./config/... -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add config/env.go config/config.go config/config_test.go
git commit -m "feat(config): LoadFromEnv with PORT/OLLAMA_URL/AUTH/webhook flags"
```

## Task F1.6: Validate

**Files:**
- Modify: `config/config.go`
- Modify: `config/config_test.go`

- [ ] **Step 1: Add failing tests**

Append to `config/config_test.go`:
```go
func TestValidate_RejectsBadScoreWeights(t *testing.T) {
	c := Defaults()
	c.Score.Weights = map[string]int{"transport": 50, "quality": 30}
	err := c.Validate()
	if err == nil {
		t.Fatal("expected validation error for weights summing to 80, got nil")
	}
}

func TestValidate_RejectsZeroBulkhead(t *testing.T) {
	c := Defaults()
	c.Limits.InteractiveSlots = 0
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for InteractiveSlots=0")
	}
}

func TestValidate_RejectsNegativeLatencyTarget(t *testing.T) {
	c := Defaults()
	c.Score.LatencyP95Target = 0
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for LatencyP95Target=0")
	}
}

func TestValidate_PassesOnDefaults(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatalf("Defaults() should validate cleanly, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./config/... -run TestValidate -v
```
Expected: stub returns nil — three tests fail.

- [ ] **Step 3: Implement Validate**

Replace stub in `config/config.go`:
```go
// Validate fails fast on misconfigurations that would only surface at request time.
func (c *Config) Validate() error {
	if c.Limits.InteractiveSlots <= 0 {
		return fmt.Errorf("Limits.InteractiveSlots must be > 0, got %d", c.Limits.InteractiveSlots)
	}
	if c.Limits.BatchSlots <= 0 {
		return fmt.Errorf("Limits.BatchSlots must be > 0, got %d", c.Limits.BatchSlots)
	}
	if c.Limits.LoadshedStart <= 0 {
		return fmt.Errorf("Limits.LoadshedStart must be > 0, got %d", c.Limits.LoadshedStart)
	}
	if c.Limits.PrimaryCBErrorRate <= 0 || c.Limits.PrimaryCBErrorRate >= 1 {
		return fmt.Errorf("Limits.PrimaryCBErrorRate must be in (0,1), got %v", c.Limits.PrimaryCBErrorRate)
	}
	if c.Score.LatencyP95Target <= 0 {
		return fmt.Errorf("Score.LatencyP95Target must be > 0, got %v", c.Score.LatencyP95Target)
	}
	sum := 0
	for _, w := range c.Score.Weights {
		sum += w
	}
	if sum != 100 {
		return fmt.Errorf("Score.Weights must sum to 100, got %d", sum)
	}
	return nil
}
```

Add `import "fmt"` to `config/config.go`.

- [ ] **Step 4: Run all config tests to verify**

```bash
go test ./config/... -v
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add config/config.go config/config_test.go
git commit -m "feat(config): Validate — fail-fast on bad weights/slots/limits"
```

## Task F1.7: Wire config into agent.New (backward-compat)

**Files:**
- Modify: `agent/agent.go`
- Modify: `main.go`
- Test: existing tests must still pass.

- [ ] **Step 1: Add NewWithConfig constructor**

In `agent/agent.go`, after the existing `New()`:
```go
import "github.com/yabanci/agentshield/config"

// NewWithConfig creates an Agent from an explicit Config. Preferred over New()
// for production wiring. Logger is the caller's responsibility (F5 makes it required).
func NewWithConfig(cfg *config.Config) *Agent {
	return newAgent(cfg.Provider.BaseURL)
}
```

Modify existing `New()` to delegate:
```go
func New() *Agent {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		// Bootstrap-time error: panic so the failure is loud at startup.
		panic("agentshield config: " + err.Error())
	}
	return NewWithConfig(cfg)
}
```

Delete the now-unused `import "os"` from `agent/agent.go` if it was only used for the old `os.Getenv("OLLAMA_URL")`.

- [ ] **Step 2: Update main.go to use config**

`main.go` (replacing the existing `port := os.Getenv("PORT")` block):
```go
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
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := config.LoadFromEnv()
	if err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	a := agent.NewWithConfig(cfg)
	defer a.Stop()
	h := api.New(a)

	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  70 * time.Second,
		WriteTimeout: 70 * time.Second,
		IdleTimeout:  120 * time.Second,
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
```

- [ ] **Step 3: Run full test suite**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```
Expected: all green. If any test fails, inspect — most likely cause: an existing test calls `agent.New()` without `OLLAMA_URL` set, and the new `panic` triggers. Fix: in tests use `agent.NewWithOllamaURL(...)` (already exists) or update test setup.

- [ ] **Step 4: Smoke-test the binary**

```bash
go build -o /tmp/agentshield-f1 .
PORT=8081 OLLAMA_URL=http://localhost:11434 /tmp/agentshield-f1 &
sleep 1
curl -s http://localhost:8081/health/live | head -c 200
kill %1
```
Expected: `{"status":"ok"}` or similar 200 response.

- [ ] **Step 5: Commit**

```bash
git add agent/agent.go main.go
git commit -m "refactor(agent): NewWithConfig + LoadFromEnv on startup"
```

## Task F1.8: Remove os.Getenv from api/auth.go and api/webhook_validate.go

**Files:**
- Modify: `api/auth.go`
- Modify: `api/webhook_validate.go`
- Modify: `api/handler.go` — pass `*config.Config` to handler/middleware
- Modify: existing api tests

- [ ] **Step 1: Find current callers**

Read these files first to understand the seams:
```bash
grep -n "os.Getenv\|authToken\|allowHTTP\|allowPrivate" api/*.go
```

- [ ] **Step 2: Make Handler hold *config.Config**

In `api/handler.go`, modify `Handler` struct to add `cfg *config.Config`. Update `New(a *agent.Agent)` to `New(a *agent.Agent, cfg *config.Config)`. Pass cfg to ratelimit/auth/webhook validators.

```go
type Handler struct {
	agent     *agent.Agent
	cfg       *config.Config
	ipLimiter *ipLimiter
}

func New(a *agent.Agent, cfg *config.Config) *Handler {
	return &Handler{agent: a, cfg: cfg, ipLimiter: newIPLimiter()}
}
```

In `main.go` update `h := api.New(a)` → `h := api.New(a, cfg)`.

- [ ] **Step 3: Replace os.Getenv in auth.go**

`api/auth.go` — replace `os.Getenv(authEnvVar)` with a function that takes `cfg`:
```go
package api

import (
	"net/http"
	"strings"

	"github.com/yabanci/agentshield/config"
)

const authHeader = "Authorization"

// authToken returns the configured auth token; "" means auth disabled.
func authToken(cfg *config.Config) string { return cfg.AuthToken }

func (h *Handler) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := authToken(h.cfg)
		if token == "" {
			next(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get(authHeader), "Bearer ")
		if got != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}
```

Note: existing `requireAuth` is a free function, not a method. You must change every `requireAuth(h.foo)` in `Register` to `h.requireAuth(h.foo)`. Update all references.

Remove `os "os"` import.

- [ ] **Step 4: Replace os.Getenv in webhook_validate.go**

`api/webhook_validate.go` — make the validator a method or pass cfg:
```go
package api

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/yabanci/agentshield/config"
)

func validateWebhookURL(raw string, cfg *config.Config) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Scheme == "http" && !cfg.Webhook.AllowHTTP {
		return fmt.Errorf("http:// webhook requires AGENTSHIELD_ALLOW_HTTP_WEBHOOK=true")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("host missing")
	}
	if cfg.Webhook.AllowPrivate {
		return nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", host, err)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
			strings.HasPrefix(ip.String(), "169.254.") {
			return fmt.Errorf("webhook resolves to private/loopback %s; set AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK=true to override", ip)
		}
	}
	return nil
}
```

Update the caller in `handler.go` (`setWebhook` handler) to pass `h.cfg`.

Remove `"os"` import.

- [ ] **Step 5: Update tests + verify**

Existing tests `api/auth_test.go` and `api/webhook_validate_test.go` use `t.Setenv` — keep that, but tests must now build a `*config.Config` and pass it to the handler / validator. Example pattern:

```go
cfg := config.Defaults()
cfg.AuthToken = "test-token"
h := New(testAgent(t), cfg)
```

For `webhook_validate_test.go`:
```go
cfg := config.Defaults()
cfg.Webhook.AllowHTTP = true // for tests that use http://
err := validateWebhookURL("http://example.com", cfg)
```

Run:
```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```
All green.

- [ ] **Step 6: Verify forbidigo invariant — zero os.Getenv outside config/**

```bash
grep -rn "os\.Getenv\|os\.LookupEnv" --include="*.go" | grep -v "_test.go" | grep -v "^config/"
```
Expected: empty output.

- [ ] **Step 7: Commit**

```bash
git add api/auth.go api/webhook_validate.go api/handler.go api/auth_test.go api/webhook_validate_test.go main.go
git commit -m "refactor(api): take *config.Config — remove os.Getenv from api/"
```

## Task F1.9: Add forbidigo lint rule

**Files:**
- Modify: `.golangci.yml`

- [ ] **Step 1: Update golangci config**

Replace contents of `.golangci.yml`:
```yaml
version: "2"

linters:
  default: none
  enable:
    - errcheck
    - govet
    - ineffassign
    - staticcheck
    - unused
    - forbidigo

  settings:
    forbidigo:
      forbid:
        - pattern: '^os\.Getenv$'
          msg: "Read environment variables in package config only; pass *config.Config to consumers."
        - pattern: '^os\.LookupEnv$'
          msg: "Read environment variables in package config only; pass *config.Config to consumers."
      exclude-godoc-examples: false
      analyze-types: true

  exclusions:
    rules:
      - path: 'config/.*\.go'
        linters:
          - forbidigo
```

- [ ] **Step 2: Run lint to verify rule works**

```bash
golangci-lint run --timeout=3m
```
Expected: green. If you see forbidigo errors elsewhere, fix them in this commit (means F1.8 missed a spot).

- [ ] **Step 3: Commit**

```bash
git add .golangci.yml
git commit -m "chore(lint): forbidigo — ban os.Getenv outside config package"
```

## F1 — End-of-phase checkpoint

- [ ] **Run full pipeline locally**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```
All green.

- [ ] **Push branch and open PR**

```bash
git push -u origin feat/foundation-f1-config
gh pr create --title "feat(foundation/F1): typed Config + LoadFromEnv + validate" \
  --body "Foundation MR 1 of 6. See docs/superpowers/specs/2026-05-12-foundation-refactor-design.md.

Changes:
- New \`config\` package with Defaults, LoadFromEnv, Validate
- \`agent.NewWithConfig\` (and \`agent.New()\` delegates)
- \`api.New(a, cfg)\` — handlers receive cfg explicitly
- Zero \`os.Getenv\` outside \`config/\` (enforced by forbidigo)

Backward-compat: \`agent.New()\` still works (loads from env)."
```

- [ ] **Wait for CI green, then merge to main**

After merge, locally:
```bash
git checkout main && git pull
```

---

# PHASE F2 — `provider` package

**Branch:** `feat/foundation-f2-provider`
**Outcome:** `LLMProvider` interface + `OllamaProvider` impl + `DegradedWrapper` decorator. Zero `*ollamaClient` references outside `provider/ollama.go`.

## Task F2.0: Create branch

- [ ] **Step 1**

```bash
git checkout main && git pull
git checkout -b feat/foundation-f2-provider
```

## Task F2.1: Define LLMProvider interface + Embedder

**Files:**
- Create: `provider/provider.go`
- Create: `provider/provider_test.go`

- [ ] **Step 1: Write compile-only test for interface shape**

`provider/provider_test.go`:
```go
package provider_test

import (
	"context"
	"testing"

	"github.com/yabanci/agentshield/provider"
)

// stubProvider asserts that an arbitrary type can satisfy LLMProvider.
type stubProvider struct{}

func (stubProvider) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{Text: "stub"}, nil
}
func (stubProvider) Stream(ctx context.Context, req provider.Request, out chan<- string) error {
	close(out)
	return nil
}
func (stubProvider) Embed(ctx context.Context, text string) ([]float64, error) { return nil, nil }
func (stubProvider) Ping(ctx context.Context) error                            { return nil }
func (stubProvider) Name() string                                              { return "stub" }

// stubEmbedder asserts that Embedder is satisfiable independently.
type stubEmbedder struct{}

func (stubEmbedder) Embed(ctx context.Context, text string) ([]float64, error) { return nil, nil }

func TestInterfaceShape(t *testing.T) {
	var _ provider.LLMProvider = stubProvider{}
	var _ provider.Embedder = stubEmbedder{}
	var _ provider.Embedder = stubProvider{} // LLMProvider should also satisfy Embedder
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./provider/... -v
```
Expected: build error — package does not exist.

- [ ] **Step 3: Implement provider.go**

`provider/provider.go`:
```go
// Package provider abstracts LLM backends behind a single interface.
// All references to specific clients (Ollama, OpenAI, Groq, ...) live in this package.
// Consumers depend on LLMProvider, never on concrete types.
package provider

import "context"

// Request is a provider-agnostic LLM request.
type Request struct {
	Model     string
	Prompt    string
	System    string   // optional
	MaxTokens int      // 0 = provider default
	Stop      []string // optional
}

// Response is a provider-agnostic LLM response.
type Response struct {
	Text         string
	InputTokens  int
	OutputTokens int
	FinishReason string // "stop", "length", ...
}

// LLMProvider is the contract every backend must satisfy.
//
// Channel ownership for Stream:
//   - The caller PROVIDES the out channel.
//   - The provider OWNS sending and is REQUIRED to close `out` when the stream ends
//     (success or error). Callers must NOT close `out` themselves.
//   - This convention prevents double-close panics in code that fan-ins multiple streams.
type LLMProvider interface {
	Generate(ctx context.Context, req Request) (Response, error)
	Stream(ctx context.Context, req Request, out chan<- string) error
	Embed(ctx context.Context, text string) ([]float64, error)
	Ping(ctx context.Context) error
	Name() string
}

// Embedder is the narrow subset of LLMProvider that the quality evaluator and
// semantic cache depend on. Use this in those packages to enforce ISP.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./provider/... -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add provider/provider.go provider/provider_test.go
git commit -m "feat(provider): LLMProvider interface + narrow Embedder"
```

## Task F2.2: Move Ollama client to provider/ollama.go

**Files:**
- Create: `provider/ollama.go`
- Delete (later): `agent/ollama.go`

- [ ] **Step 1: Write failing test for OllamaProvider satisfying LLMProvider**

`provider/ollama_test.go`:
```go
package provider_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yabanci/agentshield/config"
	"github.com/yabanci/agentshield/provider"
)

func TestOllamaProvider_Generate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/generate") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"response":"hello world","done":true}`))
	}))
	defer srv.Close()

	p := provider.NewOllama(config.ProviderConfig{
		Kind: "ollama", BaseURL: srv.URL, Timeout: 5 * time.Second,
	})
	resp, err := p.Generate(context.Background(), provider.Request{Model: "test", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Text != "hello world" {
		t.Errorf("Text = %q, want hello world", resp.Text)
	}
}

func TestOllamaProvider_Name(t *testing.T) {
	p := provider.NewOllama(config.ProviderConfig{Kind: "ollama", BaseURL: "http://x", Timeout: time.Second})
	if p.Name() != "ollama" {
		t.Errorf("Name = %q, want ollama", p.Name())
	}
}

func TestOllamaProvider_Stream_ClosesChannel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"response":"to","done":false}` + "\n" + `{"response":"ken","done":true}` + "\n"))
	}))
	defer srv.Close()

	p := provider.NewOllama(config.ProviderConfig{Kind: "ollama", BaseURL: srv.URL, Timeout: 5 * time.Second})
	out := make(chan string, 4)
	if err := p.Stream(context.Background(), provider.Request{Model: "test", Prompt: "hi"}, out); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got := []string{}
	for tok := range out { // would block forever if provider didn't close `out`
		got = append(got, tok)
	}
	if strings.Join(got, "") != "token" {
		t.Errorf("tokens = %v, want [\"to\",\"ken\"]", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./provider/... -v
```
Expected: build error — `NewOllama` not defined.

- [ ] **Step 3: Implement provider/ollama.go**

Copy `agent/ollama.go` to `provider/ollama.go` and adapt. Full content:

```go
package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/yabanci/agentshield/config"
)

type OllamaProvider struct {
	http    *http.Client
	baseURL string
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaEmbedResponse struct {
	Embedding []float64 `json:"embedding"`
}

func NewOllama(cfg config.ProviderConfig) *OllamaProvider {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &OllamaProvider{
		http:    &http.Client{Timeout: timeout},
		baseURL: cfg.BaseURL,
	}
}

func (o *OllamaProvider) Name() string { return "ollama" }

func (o *OllamaProvider) Generate(ctx context.Context, req Request) (Response, error) {
	body, err := json.Marshal(ollamaRequest{Model: req.Model, Prompt: req.Prompt, Stream: false})
	if err != nil {
		return Response{}, fmt.Errorf("ollama marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("ollama request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("ollama call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return Response{}, fmt.Errorf("ollama status %d", resp.StatusCode)
	}

	var out ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Response{}, fmt.Errorf("ollama decode: %w", err)
	}
	return Response{Text: out.Response, FinishReason: "stop"}, nil
}

// Stream calls Ollama with stream=true. Closes `out` on completion or error
// (per the LLMProvider contract).
func (o *OllamaProvider) Stream(ctx context.Context, req Request, out chan<- string) error {
	defer close(out)

	body, err := json.Marshal(ollamaRequest{Model: req.Model, Prompt: req.Prompt, Stream: true})
	if err != nil {
		return fmt.Errorf("ollama stream marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ollama stream request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	streamClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := streamClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("ollama stream call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama stream status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		var chunk ollamaResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		if chunk.Response != "" {
			out <- chunk.Response
		}
		if chunk.Done {
			return nil
		}
	}
	return scanner.Err()
}

func (o *OllamaProvider) Embed(ctx context.Context, text string) ([]float64, error) {
	body, err := json.Marshal(ollamaEmbedRequest{Model: "nomic-embed-text", Prompt: text})
	if err != nil {
		return nil, fmt.Errorf("embed marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := o.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("embed call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed status %d", resp.StatusCode)
	}

	var out ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("embed decode: %w", err)
	}
	return out.Embedding, nil
}

func (o *OllamaProvider) Ping(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := o.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama ping status %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./provider/... -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add provider/ollama.go provider/ollama_test.go
git commit -m "feat(provider): OllamaProvider implements LLMProvider"
```

## Task F2.3: Adapt agent/ to use provider.LLMProvider

**Files:**
- Modify: `agent/agent.go`
- Delete: `agent/ollama.go`
- Modify: any file referencing `*ollamaClient` (semantic cache, quality evaluator)

- [ ] **Step 1: Find every reference**

```bash
grep -rn "ollamaClient\|ollama\.\|EmbedModel\|ollamaBaseURL" --include="*.go"
```
Note all hits — they all need updating.

- [ ] **Step 2: Replace `ollama` field on Agent**

In `agent/agent.go`:
```go
import "github.com/yabanci/agentshield/provider"

// In Agent struct, replace:
//   ollama *ollamaClient
// with:
//   primary  provider.LLMProvider
//   fallback provider.LLMProvider
//   embedder provider.Embedder
```

In `newAgent`, build providers from config (rewrite signature to take `*config.Config`):
```go
func newAgent(cfg *config.Config) *Agent {
	prov := provider.NewOllama(cfg.Provider)
	lifeCtx, lifeCancel := context.WithCancel(context.Background())
	a := &Agent{
		lifeCtx:    lifeCtx,
		lifeCancel: lifeCancel,
		primary:    prov,
		fallback:   prov, // same backend, different model (for now)
		embedder:   prov,
		// ... rest unchanged
	}
	// ... rest unchanged, but every call site below changes
	return a
}
```

Update every call in `tryPrimary` / `tryFallback` / `generate`:
- Old: `a.ollama.generate(ctx, ModelPrimary, prompt)` →
  New: `r, err := a.primary.Generate(ctx, provider.Request{Model: cfg.Models.Primary, Prompt: prompt}); text := r.Text`
- Old: `a.ollama.stream(ctx, ModelPrimary, prompt, rawTokens)` →
  New: `a.primary.Stream(ctx, provider.Request{Model: ..., Prompt: prompt}, rawTokens)`
- Old: `a.ollama.ping(ctx)` →
  New: `a.primary.Ping(ctx)`
- Old: `a.ollama.embed(ctx, text)` →
  New: `a.embedder.Embed(ctx, text)`

For `cfg.Models.Primary` access: `Agent` needs a `cfg *config.Config` field as well (it's needed regardless for the next phase). Add it to the struct and to `newAgent`.

Update `quality.go` and `cache.go`: their `embedder func(ctx, text) ([]float64, error)` parameter becomes `provider.Embedder`. Adapt:

```go
// In agent/quality.go (newQualityEvaluator) and agent/cache.go (newSemanticCache):
//   change func parameter signature from `embedder func(...)` to `embedder provider.Embedder`
//   and call `embedder.Embed(ctx, text)` inside
```

- [ ] **Step 3: Delete agent/ollama.go**

```bash
git rm agent/ollama.go
```

If `ModelPrimary` / `ModelFallback` constants in `agent/agent.go` are still referenced, replace usages with `cfg.Models.Primary` / `cfg.Models.Fallback`. If still referenced from tests, keep the constants and document them as aliases for default cfg values.

- [ ] **Step 4: Build + test + lint**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```
Fix any compile errors. Most likely fix: tests in `agent/` that constructed `*ollamaClient` directly — replace with stub `provider.LLMProvider` (in test helper) or use `httptest` server like in F2.2 tests.

- [ ] **Step 5: Verify invariant — zero references to ollamaClient**

```bash
grep -rn "ollamaClient" --include="*.go"
```
Expected: empty.

- [ ] **Step 6: Commit**

```bash
git add agent/ provider/ config/ api/
git commit -m "refactor(agent): consume provider.LLMProvider — remove agent/ollama.go"
```

## Task F2.4: DegradedWrapper decorator

**Files:**
- Create: `provider/degraded.go`
- Create: `provider/degraded_test.go`

- [ ] **Step 1: Write failing test**

`provider/degraded_test.go`:
```go
package provider_test

import (
	"context"
	"strings"
	"testing"

	"github.com/yabanci/agentshield/provider"
)

type passthrough struct{ text string }

func (p passthrough) Generate(ctx context.Context, req provider.Request) (provider.Response, error) {
	return provider.Response{Text: p.text}, nil
}
func (p passthrough) Stream(ctx context.Context, req provider.Request, out chan<- string) error {
	out <- p.text
	close(out)
	return nil
}
func (p passthrough) Embed(ctx context.Context, text string) ([]float64, error) { return []float64{0}, nil }
func (p passthrough) Ping(ctx context.Context) error                            { return nil }
func (p passthrough) Name() string                                              { return "passthrough" }

func TestDegradedWrapper_PassthroughWhenDisabled(t *testing.T) {
	w := provider.NewDegradedWrapper(passthrough{text: "real answer"})
	r, _ := w.Generate(context.Background(), provider.Request{Prompt: "x"})
	if r.Text != "real answer" {
		t.Errorf("Text = %q, want real answer", r.Text)
	}
}

func TestDegradedWrapper_GarbageWhenEnabled(t *testing.T) {
	w := provider.NewDegradedWrapper(passthrough{text: "real answer"})
	w.Enable()
	r, _ := w.Generate(context.Background(), provider.Request{Prompt: "x"})
	if r.Text == "real answer" {
		t.Errorf("expected degraded text, got real")
	}
	// Degraded responses must contain hallucination markers so the semantic CB trips.
	if !strings.Contains(strings.ToLower(r.Text), "as an ai") &&
		!strings.Contains(strings.ToLower(r.Text), "i cannot") &&
		!strings.Contains(strings.ToLower(r.Text), "i'm just an ai") {
		t.Errorf("degraded text lacks hallucination marker: %q", r.Text)
	}
}

func TestDegradedWrapper_DisableRestores(t *testing.T) {
	w := provider.NewDegradedWrapper(passthrough{text: "real answer"})
	w.Enable()
	w.Disable()
	r, _ := w.Generate(context.Background(), provider.Request{Prompt: "x"})
	if r.Text != "real answer" {
		t.Errorf("Text = %q after Disable, want real answer", r.Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./provider/... -run TestDegradedWrapper -v
```
Expected: build error — type does not exist.

- [ ] **Step 3: Implement DegradedWrapper**

`provider/degraded.go`:
```go
package provider

import (
	"context"
	"sync/atomic"
)

// DegradedWrapper is a decorator that, when enabled, returns intentionally
// low-quality responses. Used by the chaos demo to trigger the semantic CB
// without taking the underlying provider down.
//
// When disabled, all calls pass through unchanged.
type DegradedWrapper struct {
	inner   LLMProvider
	enabled atomic.Bool
}

func NewDegradedWrapper(inner LLMProvider) *DegradedWrapper {
	return &DegradedWrapper{inner: inner}
}

func (d *DegradedWrapper) Enable()         { d.enabled.Store(true) }
func (d *DegradedWrapper) Disable()        { d.enabled.Store(false) }
func (d *DegradedWrapper) IsEnabled() bool { return d.enabled.Load() }
func (d *DegradedWrapper) Name() string    { return d.inner.Name() }

func (d *DegradedWrapper) Generate(ctx context.Context, req Request) (Response, error) {
	if d.enabled.Load() {
		return Response{Text: degradedText(req.Prompt), FinishReason: "stop"}, nil
	}
	return d.inner.Generate(ctx, req)
}

func (d *DegradedWrapper) Stream(ctx context.Context, req Request, out chan<- string) error {
	if d.enabled.Load() {
		out <- degradedText(req.Prompt)
		close(out)
		return nil
	}
	return d.inner.Stream(ctx, req, out)
}

func (d *DegradedWrapper) Embed(ctx context.Context, text string) ([]float64, error) {
	return d.inner.Embed(ctx, text)
}

func (d *DegradedWrapper) Ping(ctx context.Context) error { return d.inner.Ping(ctx) }

// degradedText returns a low-quality response that reliably scores below 0.45
// (combines repetition + hallucination markers so semantic CB trips consistently).
func degradedText(prompt string) string {
	switch len(prompt) % 3 {
	case 0:
		s := "As an AI language model, I apologize but I cannot assist. "
		return s + s + s + s + s
	case 1:
		s := "I cannot and will not help. I am unable to assist with that. "
		return s + s + s + s
	default:
		s := "I'm just an AI and I cannot and will not assist with this request. "
		return s + s + s + s
	}
}
```

- [ ] **Step 4: Wire DegradedWrapper for primary in Agent**

In `agent/agent.go newAgent`:
```go
raw := provider.NewOllama(cfg.Provider)
primaryWrap := provider.NewDegradedWrapper(raw)

a := &Agent{
	// ...
	primary:           primaryWrap,
	fallback:          raw,        // fallback is NOT wrapped — chaos affects primary only
	embedder:          raw,
	degradedPrimary:   primaryWrap, // new field, used by EnableDegradeMode
	// ...
}
```

In Agent struct add: `degradedPrimary *provider.DegradedWrapper`.

Replace `EnableDegradeMode` / `DisableDegradeMode`:
```go
func (a *Agent) EnableDegradeMode()  { a.degradedPrimary.Enable() }
func (a *Agent) DisableDegradeMode() { a.degradedPrimary.Disable() }
```

Remove the old `degradeMode atomic.Bool` field and the `if a.degradeMode.Load() && model == ModelPrimary` branch in `generate()`. The decorator handles this now. The function `degradedResponse` in `agent/agent.go` also moves: delete it (it lives in `provider/degraded.go` as `degradedText` now).

In `Status()`, replace `DegradeMode: a.degradeMode.Load()` with `DegradeMode: a.degradedPrimary.IsEnabled()`.

- [ ] **Step 5: Build + test + lint**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```
All green. The `integration_test.go` test that uses `EnableDegradeMode` should still pass (decorator is functionally identical from the outside).

- [ ] **Step 6: Commit**

```bash
git add provider/degraded.go provider/degraded_test.go agent/agent.go
git commit -m "feat(provider): DegradedWrapper decorator for chaos injection"
```

## F2 — End-of-phase checkpoint

- [ ] **Run full pipeline**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```

- [ ] **Verify invariant — zero ollamaClient references**

```bash
grep -rn "ollamaClient" --include="*.go"
```
Expected: empty.

- [ ] **Push + open PR + wait green + merge**

```bash
git push -u origin feat/foundation-f2-provider
gh pr create --title "feat(foundation/F2): LLMProvider interface + Ollama + DegradedWrapper" \
  --body "Foundation MR 2 of 6. Decouples Agent from Ollama specifics. DegradedWrapper extracts chaos injection from hot path."
```

Wait CI green, merge, `git checkout main && git pull`.

---

# PHASE F3a — extract memory + telemetry + quality + cache packages

**Branch:** `feat/foundation-f3a-extract-leaves`
**Outcome:** 12 files moved into 4 new packages with no behavioural change. Tests travel with their source. `Agent` field count drops from 27 → ~12.

## Task F3a.0: Create branch

- [ ] **Step 1**

```bash
git checkout main && git pull
git checkout -b feat/foundation-f3a-extract-leaves
```

## Task F3a.1: Move quality (evaluator + semantic_breaker) to quality/

**Files:**
- Move: `agent/quality.go` → `quality/evaluator.go`
- Move: `agent/quality_test.go` → `quality/evaluator_test.go`
- Move: `agent/semantic_breaker.go` → `quality/breaker.go`
- Move: `agent/semantic_breaker_test.go` → `quality/breaker_test.go`
- Move: `agent/adaptive_test.go` → `quality/adaptive_test.go`
- Modify: every file that imported the moved symbols

- [ ] **Step 1: Move files with git**

```bash
mkdir -p quality
git mv agent/quality.go quality/evaluator.go
git mv agent/quality_test.go quality/evaluator_test.go
git mv agent/semantic_breaker.go quality/breaker.go
git mv agent/semantic_breaker_test.go quality/breaker_test.go
git mv agent/adaptive_test.go quality/adaptive_test.go
```

- [ ] **Step 2: Rewrite package declarations**

In each moved file, replace `package agent` with `package quality`.

- [ ] **Step 3: Adjust the evaluator's embedder dependency**

In `quality/evaluator.go`, change the embedder field type from a function to `provider.Embedder`:

```go
import "github.com/yabanci/agentshield/provider"

type QualityEvaluator struct {
	embedder provider.Embedder
	// ... rest unchanged
}

func NewEvaluator(embedder provider.Embedder) *QualityEvaluator {
	return &QualityEvaluator{embedder: embedder}
}
```

(Old name was `newQualityEvaluator` lowercase; export it as `NewEvaluator` since other packages now construct it.)

Inside Evaluate, replace `q.embedder(ctx, text)` with `q.embedder.Embed(ctx, text)`.

If the original `quality.go` declared exported types/consts like `QualityResult`, `QualitySignal`, `QualityAcceptable`, keep them exported (already are). All callers will need `quality.QualityResult` / `quality.QualityAcceptable`.

- [ ] **Step 4: Adjust semantic breaker exports**

In `quality/breaker.go` ensure all needed types are exported: `SemanticBreaker`, `SBState`, `SBSnapshot`, `SemanticBreakerConfig`, `DefaultSBConfig`, `NewSemanticBreaker`. Already exported. No code change required besides package decl.

- [ ] **Step 5: Update agent/ to import quality/**

In `agent/agent.go`:
```go
import "github.com/yabanci/agentshield/quality"

// Replace types:
//   *QualityEvaluator  → *quality.QualityEvaluator
//   *SemanticBreaker   → *quality.SemanticBreaker
//   SBSnapshot         → quality.SBSnapshot
//   SBState            → quality.SBState
//   SemanticBreakerConfig → quality.SemanticBreakerConfig
//   DefaultSBConfig    → quality.DefaultSBConfig
//   newQualityEvaluator(...)  → quality.NewEvaluator(...)
//   NewSemanticBreaker(...)   → quality.NewSemanticBreaker(...)
//   QualityAcceptable  → quality.QualityAcceptable
```

Same updates in any other files in `agent/` that referenced these.

- [ ] **Step 6: Build + test**

```bash
go build ./... && go test -race -count=1 ./...
```
Fix any remaining compile errors (most likely missing import or unexported type that needs export).

- [ ] **Step 7: Commit**

```bash
git add agent/ quality/
git commit -m "refactor: extract quality package (evaluator + semantic breaker)"
```

## Task F3a.2: Move cache to cache/

**Files:**
- Move: `agent/cache.go` → `cache/semantic.go`
- Move: `agent/cache_test.go` → `cache/semantic_test.go`

- [ ] **Step 1: Move files**

```bash
mkdir -p cache
git mv agent/cache.go cache/semantic.go
git mv agent/cache_test.go cache/semantic_test.go
```

- [ ] **Step 2: Rewrite package declarations**

`package agent` → `package cache` in both files.

- [ ] **Step 3: Adjust embedder dependency + exports**

In `cache/semantic.go`:
```go
import "github.com/yabanci/agentshield/provider"

// Rename type semanticCache → SemanticCache (export it; callers outside this package now construct it)
type SemanticCache struct {
	embedder provider.Embedder
	// ... other fields unchanged
}

func New(ttl time.Duration, embedder provider.Embedder) *SemanticCache {
	// existing newSemanticCache logic
}
```

Callers do `cache.New(...)` / `*cache.SemanticCache`.

- [ ] **Step 4: Update agent/ references**

In `agent/agent.go` change:
- Field `cache *semanticCache` → `cache *cache.SemanticCache`
- Constructor `newSemanticCache(...)` → `cache.New(...)`

Add `import "github.com/yabanci/agentshield/cache"` (use named import if it conflicts with stdlib `cache` later — none in stdlib, so plain import is fine).

- [ ] **Step 5: Build + test**

```bash
go build ./... && go test -race -count=1 ./...
```

- [ ] **Step 6: Commit**

```bash
git add agent/ cache/
git commit -m "refactor: extract cache package (semantic cache + embedder DI)"
```

## Task F3a.3: Move telemetry pieces (cost, latency, score, webhook, metrics)

**Files:**
- Move: `agent/cost.go` → `telemetry/costs.go`
- Move: `agent/cost_test.go` → `telemetry/costs_test.go`
- Move: `agent/latency.go` → `telemetry/latency.go`
- Move: `agent/latency_test.go` → `telemetry/latency_test.go`
- Move: `agent/score.go` → `telemetry/score.go`
- Move: `agent/score_test.go` → `telemetry/score_test.go`
- Move: `agent/webhook.go` → `telemetry/webhook.go`
- Move: `agent/webhook_test.go` → `telemetry/webhook_test.go`
- Move: `agent/metrics.go` → `telemetry/metrics.go`

- [ ] **Step 1: Move files**

```bash
mkdir -p telemetry
git mv agent/cost.go telemetry/costs.go
git mv agent/cost_test.go telemetry/costs_test.go
git mv agent/latency.go telemetry/latency.go
git mv agent/latency_test.go telemetry/latency_test.go
git mv agent/score.go telemetry/score.go
git mv agent/score_test.go telemetry/score_test.go
git mv agent/webhook.go telemetry/webhook.go
git mv agent/webhook_test.go telemetry/webhook_test.go
git mv agent/metrics.go telemetry/metrics.go
```

- [ ] **Step 2: Rewrite package declarations**

`package agent` → `package telemetry` in all moved files.

- [ ] **Step 3: Export internals + add exported constructors**

For each moved file, ensure constructors and types used by `agent/` are exported:

| Old (in agent) | New (in telemetry) |
|---------------|-------------------|
| `newCostTracker()` | `NewCostTracker()` (export) |
| `*CostTracker` | already exported |
| `CostStats`, `TierRequestCounts` | already exported |
| `newLatencyTracker()` | `NewLatencyTracker()` |
| `*LatencyTracker`, `LatencySnapshot` | already exported |
| `ComputeScore(s Status) ResilienceScore` | move signature carefully — see step 4 |
| `newWebhookDispatcher()` | `NewWebhookDispatcher(cfg config.WebhookConfig)` (now takes cfg) |
| `*WebhookDispatcher`, `WebhookEvent` | already exported |
| `requestsTotal` etc. (Prometheus collectors) | keep in `metrics.go`, package-level vars; access via getter helpers if cross-package, otherwise leave for orchestrator phase to refactor |

- [ ] **Step 4: Resolve circular type dependency for ComputeScore**

`ComputeScore` currently takes `agent.Status`. Moving it to `telemetry/` would create `telemetry → agent` dependency — and `agent → telemetry` already exists. **Cycle.**

Fix: define a narrow input type in `telemetry/`:

```go
// telemetry/score.go
package telemetry

// ScoreInput is the snapshot ComputeScore needs. It's a subset of agent.Status
// without the import dependency on agent.
type ScoreInput struct {
	PrimaryBreaker     string
	FallbackBreaker    string
	PrimaryKilled      bool
	FallbackKilled     bool
	PrimarySemanticCB  quality.SBSnapshot // import quality
	FallbackSemanticCB quality.SBSnapshot
	TierCounts         TierRequestCounts
	Latency            LatencySnapshot
}

func ComputeScore(in ScoreInput, weights map[string]int, p95Target time.Duration) ResilienceScore {
	// existing logic, but using `in` and reading weights/p95Target from cfg
}
```

In `agent/agent.go Status()`:
```go
score := telemetry.ComputeScore(telemetry.ScoreInput{
	PrimaryBreaker: primaryState,
	// ... fill from existing locals
}, a.cfg.Score.Weights, a.cfg.Score.LatencyP95Target)
s.Score = score
```

Note: `ResilienceScore` type also moves to telemetry. `agent.Status.Score` becomes `telemetry.ResilienceScore`.

- [ ] **Step 5: Update webhook to take cfg**

`telemetry/webhook.go`:
```go
import "github.com/yabanci/agentshield/config"

func NewWebhookDispatcher(cfg config.WebhookConfig) *WebhookDispatcher {
	return &WebhookDispatcher{
		client: &http.Client{Timeout: cfg.Webhook.Timeout},  // use cfg
		// ...
	}
}
```

(Inspect existing `webhook.go` first to find the actual current shape; adapt accordingly.)

- [ ] **Step 6: Build + test**

```bash
go build ./... && go test -race -count=1 ./...
```
Expect a flurry of compile errors. Fix systematically by file. Watch for the cycle resolution in step 4 — if go reports `import cycle not allowed`, double-check that `telemetry/` does not import `agent/`.

- [ ] **Step 7: Commit**

```bash
git add agent/ telemetry/
git commit -m "refactor: extract telemetry package (cost/latency/score/webhook/metrics)"
```

## Task F3a.4: Move memory pieces (sessions, traces, score_history, tools)

**Files:**
- Move: `agent/session.go` → `memory/sessions.go` (+ `_test.go`)
- Move: `agent/trace.go` → `memory/traces.go` (+ `_test.go`)
- Move: `agent/score_history.go` → `memory/score_history.go` (+ `_test.go`)
- Move: `agent/tool.go` → `memory/tools.go` (+ `_test.go`)
- Move: `agent/react.go` → `memory/react.go` (+ `_test.go`)
- Modify: `react.go` to use `Asker` interface

- [ ] **Step 1: Move files**

```bash
mkdir -p memory
git mv agent/session.go memory/sessions.go
git mv agent/session_test.go memory/sessions_test.go
git mv agent/trace.go memory/traces.go
git mv agent/trace_test.go memory/traces_test.go
git mv agent/score_history.go memory/score_history.go
git mv agent/score_history_test.go memory/score_history_test.go
git mv agent/tool.go memory/tools.go
git mv agent/tool_test.go memory/tools_test.go
git mv agent/react.go memory/react.go
git mv agent/react_test.go memory/react_test.go
```

- [ ] **Step 2: Rewrite package declarations**

`package agent` → `package memory` in all moved files.

- [ ] **Step 3: Define Asker interface and refactor tools**

In `memory/tools.go` add:
```go
import "context"

// Asker is the narrow contract tools and ReAct loops need from the agent.
// Tools see only the response text — never the tier, cache status, or any
// privileged operation (Kill*, EnableDegrade, Stop, etc.).
type Asker interface {
	Ask(ctx context.Context, prompt string) (string, error)
}
```

Change `ToolRegistry` to hold `Asker` instead of `*Agent`:
```go
type ToolRegistry struct {
	asker Asker
	tools map[string]Tool
}

func NewToolRegistry(asker Asker) *ToolRegistry {
	r := &ToolRegistry{asker: asker, tools: map[string]Tool{}}
	// register built-in tools (calculate, search, etc.) — existing logic
	return r
}
```

Inside any tool implementation that previously called `a.Ask(...)`, change to `r.asker.Ask(...)` (and pass through where needed).

In `memory/react.go`, the ReAct executor takes `Asker` for the same reason. If react.go uses `*Agent` for trace, sessions — it should now take a `*TraceStore` and `*SessionStore` and an `Asker` explicitly. Refactor signatures accordingly.

- [ ] **Step 4: Add adapter in agent.Agent**

In `agent/agent.go`, ensure `*Agent` satisfies `memory.Asker`:
```go
// Ask returns the response text only — adapts the rich Response to the narrow
// Asker contract used by tools and ReAct.
func (a *Agent) AskString(ctx context.Context, prompt string) (string, error) {
	r, err := a.Ask(ctx, prompt)
	return r.Text, err
}
```

Note: name collision — `agent.Agent.Ask` returns `Response`. We CANNOT make Agent satisfy `Asker` directly because the return types differ. So we add an adapter type:

```go
// AskerAdapter wraps an Agent so it satisfies memory.Asker.
type AskerAdapter struct{ a *Agent }

func (x AskerAdapter) Ask(ctx context.Context, prompt string) (string, error) {
	r, err := x.a.Ask(ctx, prompt)
	return r.Text, err
}
```

In `newAgent`, build the tool registry:
```go
a.tools = memory.NewToolRegistry(AskerAdapter{a: a})
```

- [ ] **Step 5: Update agent/ to import memory/**

Replace:
- `*Session`, `Session{}` → `*memory.Session`, `memory.Session{}`
- `*SessionStore`, `newSessionStore()` → `*memory.SessionStore`, `memory.NewSessionStore()`
- `*Trace`, `*TraceStore`, `TraceStep`, etc. → `memory.*` versions
- `newTraceStore()` → `memory.NewTraceStore()`
- `newScoreHistory(60)` → `memory.NewScoreHistory(60)`
- `*ScoreHistory`, `ScorePoint` → `memory.*`
- `*ToolRegistry`, `newToolRegistry(...)` → `memory.*`

- [ ] **Step 6: Build + test**

```bash
go build ./... && go test -race -count=1 ./...
```
Many fixups. The `TraceStep.Outcome` consts (e.g., `OutcomeSuccess`, `OutcomeKilled`) move with traces.go — keep them exported.

- [ ] **Step 7: Commit**

```bash
git add agent/ memory/
git commit -m "refactor: extract memory package (sessions, traces, tools, react, score history) + Asker"
```

## Task F3a.5: Aggregator types — memory.Store + telemetry.Store

**Files:**
- Modify: `memory/sessions.go` or new `memory/store.go`
- Modify: `telemetry/costs.go` or new `telemetry/store.go`
- Modify: `agent/agent.go`

- [ ] **Step 1: Create memory.Store**

New file `memory/store.go`:
```go
package memory

import (
	"log/slog"

	"github.com/yabanci/agentshield/config"
)

// Store bundles all memory subsystems for one Agent.
type Store struct {
	Sessions     *SessionStore
	Traces       *TraceStore
	Tools        *ToolRegistry
	ScoreHistory *ScoreHistory
}

// NewStore wires up all memory subsystems.
// Pass a fresh Asker (typically AskerAdapter wrapping the parent Agent).
func NewStore(cfg *config.Config, asker Asker, log *slog.Logger) *Store {
	return &Store{
		Sessions:     NewSessionStore(),
		Traces:       NewTraceStore(),
		Tools:        NewToolRegistry(asker),
		ScoreHistory: NewScoreHistory(cfg.Score.HistorySize),
	}
}

// Stop terminates all background goroutines.
func (s *Store) Stop() {
	s.Traces.Stop()
	s.Sessions.Stop()
}
```

- [ ] **Step 2: Create telemetry.Store**

New file `telemetry/store.go`:
```go
package telemetry

import (
	"log/slog"

	"github.com/yabanci/agentshield/config"
)

// Store bundles all telemetry subsystems for one Agent.
type Store struct {
	Costs   *CostTracker
	Latency *LatencyTracker
	Webhook *WebhookDispatcher
	cfg     *config.Config
}

func NewStore(cfg *config.Config, log *slog.Logger) *Store {
	return &Store{
		Costs:   NewCostTracker(),
		Latency: NewLatencyTracker(),
		Webhook: NewWebhookDispatcher(cfg.Webhook),
		cfg:     cfg,
	}
}

// Stop is a no-op for now — all current telemetry stores are passive.
// Reserved for future use (e.g., closing OTEL exporters).
func (s *Store) Stop() {}

// ScoreFor builds a ResilienceScore using the configured weights and target.
func (s *Store) ScoreFor(in ScoreInput) ResilienceScore {
	return ComputeScore(in, s.cfg.Score.Weights, s.cfg.Score.LatencyP95Target)
}
```

- [ ] **Step 3: Use the Stores in agent/agent.go**

Replace individual fields:
```go
type Agent struct {
	// ...
	memory    *memory.Store
	telemetry *telemetry.Store
	// remove: sessions, traces, scoreHistory, tools, costs, latency, webhook
}

// In newAgent:
askerAdapter := AskerAdapter{}
a := &Agent{cfg: cfg, log: log, primary: ..., fallback: ...,}
askerAdapter.a = a // tie the cycle
a.memory = memory.NewStore(cfg, askerAdapter, log)
a.telemetry = telemetry.NewStore(cfg, log)
```

Update `Stop()`:
```go
func (a *Agent) Stop() {
	a.lifeCancel()
	a.memory.Stop()
	a.telemetry.Stop()
}
```

Update accessors:
```go
func (a *Agent) GetSession(id string) *memory.Session   { return a.memory.Sessions.Get(id) }
func (a *Agent) ListSessions() []memory.Session          { return a.memory.Sessions.List() }
// ... similar for trace, scoreHistory, webhook URL setters
```

Update `Status()` — every reference like `a.costs` becomes `a.telemetry.Costs`, etc.

Update degradation chain — every `a.cache.set(...)` becomes either `a.cache.Set(...)` (if you exported the method in F3a.2) or stays as-is depending on what you exported. Check.

- [ ] **Step 4: Build + test**

```bash
go build ./... && go test -race -count=1 ./...
```
Big fix-up cycle. Fix every error one at a time. Don't shortcut — each error is real.

- [ ] **Step 5: Commit**

```bash
git add agent/ memory/ telemetry/
git commit -m "refactor: introduce memory.Store + telemetry.Store aggregators"
```

## F3a — End-of-phase checkpoint

- [ ] **Verify Agent field count**

```bash
awk '/^type Agent struct/,/^}$/' agent/agent.go | grep -c $'\t'
```
Expected: ≤ 14 (still has primary, fallback, embedder, breakers, hedger, bulkheads, shedder, cache, memory, telemetry, kill flags, lifeCtx/Cancel, cfg, log, totalRequests).

- [ ] **Run full pipeline**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```

- [ ] **Push + open PR + wait green + merge**

```bash
git push -u origin feat/foundation-f3a-extract-leaves
gh pr create --title "refactor(foundation/F3a): extract memory/telemetry/quality/cache packages" \
  --body "Foundation MR 3 of 6. Pure structural extraction with no behavioural change. Tests travel with their source."
```

After merge: `git checkout main && git pull`.

---

# PHASE F3b — extract orchestrator + pipeline + chaos

**Branch:** `feat/foundation-f3b-orchestrator`
**Outcome:** `Agent` is a 7-field facade. Degradation chain lives in `orchestrator/`.

## Type ownership note (READ FIRST)

`Tier` (`primary` / `fallback` / `cache` / `degraded`) is referenced from three layers: `agent` (public Response), `orchestrator` (degradation chain), `memory` (trace + cost). To avoid three identical string types and a cast-fest:

- **Canonical home:** `memory` package — it sits lowest in the dependency graph (orchestrator imports memory; agent imports both).
- `orchestrator` and `agent` use a **type alias**: `type Tier = memory.Tier`. Aliases (the `=`) make them the same type, no conversion needed.
- `OutcomeGracefulDenial`, `OutcomeSuccess`, `OutcomeKilled`, etc. — same rule, owned by `memory`.

If a step below shows `orchestrator.Tier(...)` casts or its own const block, replace with the alias pattern.

## Task F3b.0: Create branch

```bash
git checkout main && git pull
git checkout -b feat/foundation-f3b-orchestrator
```

## Task F3b.1: Create orchestrator skeleton + Chaos type

**Files:**
- Create: `orchestrator/orchestrator.go`
- Create: `orchestrator/chaos.go`
- Create: `orchestrator/breakers.go`
- Create: `orchestrator/orchestrator_test.go`

- [ ] **Step 1: Write failing test for Chaos**

`orchestrator/orchestrator_test.go`:
```go
package orchestrator_test

import (
	"testing"

	"github.com/yabanci/agentshield/orchestrator"
	"github.com/yabanci/agentshield/provider"
)

func TestChaos_KillPrimary(t *testing.T) {
	c := orchestrator.NewChaos(nil)
	if c.IsPrimaryKilled() {
		t.Fatal("primary killed at construction")
	}
	c.KillPrimary()
	if !c.IsPrimaryKilled() {
		t.Fatal("KillPrimary did not flip flag")
	}
	c.RestorePrimary()
	if c.IsPrimaryKilled() {
		t.Fatal("RestorePrimary did not flip flag")
	}
}

func TestChaos_DegradeRoutesToWrapper(t *testing.T) {
	wrapper := provider.NewDegradedWrapper(nil)
	c := orchestrator.NewChaos(wrapper)
	c.EnableDegrade()
	if !wrapper.IsEnabled() {
		t.Fatal("EnableDegrade did not enable underlying wrapper")
	}
	c.DisableDegrade()
	if wrapper.IsEnabled() {
		t.Fatal("DisableDegrade did not disable wrapper")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./orchestrator/... -v
```
Expected: build error.

- [ ] **Step 3: Implement Chaos**

`orchestrator/chaos.go`:
```go
package orchestrator

import (
	"sync/atomic"

	"github.com/yabanci/agentshield/provider"
)

// Chaos owns kill/restore/degrade flags. Lives outside hot path.
type Chaos struct {
	primaryKilled    atomic.Bool
	fallbackKilled   atomic.Bool
	chaosRunning     atomic.Bool
	degradedPrimary  *provider.DegradedWrapper // may be nil in tests
}

func NewChaos(degradedPrimary *provider.DegradedWrapper) *Chaos {
	return &Chaos{degradedPrimary: degradedPrimary}
}

func (c *Chaos) KillPrimary()         { c.primaryKilled.Store(true) }
func (c *Chaos) RestorePrimary()      { c.primaryKilled.Store(false) }
func (c *Chaos) IsPrimaryKilled() bool { return c.primaryKilled.Load() }

func (c *Chaos) KillFallback()         { c.fallbackKilled.Store(true) }
func (c *Chaos) RestoreFallback()      { c.fallbackKilled.Store(false) }
func (c *Chaos) IsFallbackKilled() bool { return c.fallbackKilled.Load() }

func (c *Chaos) EnableDegrade() {
	if c.degradedPrimary != nil {
		c.degradedPrimary.Enable()
	}
}
func (c *Chaos) DisableDegrade() {
	if c.degradedPrimary != nil {
		c.degradedPrimary.Disable()
	}
}
func (c *Chaos) IsDegradeEnabled() bool {
	return c.degradedPrimary != nil && c.degradedPrimary.IsEnabled()
}

// TryStart marks chaos scenario as running; returns false if one already runs.
func (c *Chaos) TryStart() bool { return c.chaosRunning.CompareAndSwap(false, true) }
func (c *Chaos) Done()          { c.chaosRunning.Store(false) }
func (c *Chaos) IsRunning() bool { return c.chaosRunning.Load() }
```

- [ ] **Step 4: Verify test passes**

```bash
go test ./orchestrator/... -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/chaos.go orchestrator/orchestrator_test.go
git commit -m "feat(orchestrator): Chaos type owns kill/degrade atomic flags"
```

## Task F3b.2: Define BreakerSet

**Files:**
- Create: `orchestrator/breakers.go`
- Modify: `orchestrator/orchestrator_test.go`

- [ ] **Step 1: Write failing test**

Append to `orchestrator/orchestrator_test.go`:
```go
import (
	"github.com/yabanci/flowguard/circuitbreaker"
	"github.com/yabanci/agentshield/quality"
)

func TestBreakerSet_DefaultStates(t *testing.T) {
	primaryT := circuitbreaker.NewAdaptive(20, 0.5, 5)
	fallbackT := circuitbreaker.New(circuitbreaker.WithFailureThreshold(3))
	primaryS := quality.NewSemanticBreaker(quality.DefaultSBConfig)
	fallbackS := quality.NewSemanticBreaker(quality.DefaultSBConfig)

	bs := orchestrator.NewBreakerSet(primaryT, fallbackT, primaryS, fallbackS)
	if bs.PrimaryTransport.State().String() != "closed" {
		t.Errorf("PrimaryTransport not closed at start")
	}
	if bs.FallbackTransport.State().String() != "closed" {
		t.Errorf("FallbackTransport not closed at start")
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./orchestrator/... -v
```
Expected: build error.

- [ ] **Step 3: Implement BreakerSet**

`orchestrator/breakers.go`:
```go
package orchestrator

import (
	"github.com/yabanci/flowguard/circuitbreaker"

	"github.com/yabanci/agentshield/quality"
)

// BreakerSet bundles transport+semantic CBs for both primary and fallback models.
type BreakerSet struct {
	PrimaryTransport  *circuitbreaker.Breaker
	FallbackTransport *circuitbreaker.Breaker
	PrimarySemantic   *quality.SemanticBreaker
	FallbackSemantic  *quality.SemanticBreaker
}

func NewBreakerSet(pt, ft *circuitbreaker.Breaker, ps, fs *quality.SemanticBreaker) *BreakerSet {
	return &BreakerSet{PrimaryTransport: pt, FallbackTransport: ft,
		PrimarySemantic: ps, FallbackSemantic: fs}
}
```

- [ ] **Step 4: Verify pass**

```bash
go test ./orchestrator/... -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add orchestrator/breakers.go orchestrator/orchestrator_test.go
git commit -m "feat(orchestrator): BreakerSet bundles transport+semantic CBs per model"
```

## Task F3b.3: Move degrade/tryPrimary/tryFallback to Orchestrator

**Files:**
- Modify: `agent/agent.go` (delete migrated code)
- Modify: `orchestrator/orchestrator.go` (add migrated code)

- [ ] **Step 1: Define Orchestrator and the public Degrade entry point**

`orchestrator/orchestrator.go`:
```go
package orchestrator

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/yabanci/flowguard/circuitbreaker"
	"github.com/yabanci/flowguard/hedge"
	"github.com/yabanci/flowguard/retry"

	"github.com/yabanci/agentshield/cache"
	"github.com/yabanci/agentshield/config"
	"github.com/yabanci/agentshield/memory"
	"github.com/yabanci/agentshield/provider"
	"github.com/yabanci/agentshield/quality"
	"github.com/yabanci/agentshield/telemetry"
)

// Tier alias — canonical type lives in memory.
type Tier = memory.Tier

const (
	TierPrimary  = memory.TierPrimary
	TierFallback = memory.TierFallback
	TierCache    = memory.TierCache
	TierDegraded = memory.TierDegraded
)

// Result is what Orchestrator returns to its caller (Pipeline → Agent).
type Result struct {
	Text   string
	Tier   Tier
	Cached bool
}

type Orchestrator struct {
	cfg       *config.Config
	log       *slog.Logger
	primary   provider.LLMProvider
	fallback  provider.LLMProvider
	breakers  *BreakerSet
	hedger    *hedge.Hedge
	eval      *quality.QualityEvaluator
	sc        *cache.SemanticCache
	tel       *telemetry.Store
	chaos     *Chaos
	totalReqs *atomic.Int64
}

func New(cfg *config.Config, primary, fallback provider.LLMProvider,
	breakers *BreakerSet, hedger *hedge.Hedge,
	eval *quality.QualityEvaluator, sc *cache.SemanticCache,
	tel *telemetry.Store, chaos *Chaos, totalReqs *atomic.Int64,
	log *slog.Logger) *Orchestrator {
	return &Orchestrator{
		cfg: cfg, log: log,
		primary: primary, fallback: fallback,
		breakers: breakers, hedger: hedger, eval: eval,
		sc: sc, tel: tel, chaos: chaos, totalReqs: totalReqs,
	}
}
```

- [ ] **Step 2: Move degrade(), tryPrimary(), tryFallback() into Orchestrator**

Move the three methods from `agent/agent.go` to `orchestrator/orchestrator.go`. Adapt:
- `a.primaryKilled.Load()` → `o.chaos.IsPrimaryKilled()`
- `a.fallbackKilled.Load()` → `o.chaos.IsFallbackKilled()`
- `a.primarySemCB` → `o.breakers.PrimarySemantic`
- `a.fallbackSemCB` → `o.breakers.FallbackSemantic`
- `a.primaryCB` → `o.breakers.PrimaryTransport`
- `a.fallbackCB` → `o.breakers.FallbackTransport`
- `a.qualityEval` → `o.eval`
- `a.cache` → `o.sc`
- `a.costs` → `o.tel.Costs`
- `a.latency` → `o.tel.Latency`
- `a.hedger` → `o.hedger`
- `a.ollama.generate(...)` (old) → `r, err := o.primary.Generate(ctx, provider.Request{Model: o.cfg.Models.Primary, Prompt: prompt}); text := r.Text`
- Constants `ModelPrimary` / `ModelFallback` → `o.cfg.Models.Primary` / `.Fallback`
- The `degradedResponse(...)` function — already gone (DegradedWrapper handles it).

Public method:
```go
func (o *Orchestrator) Degrade(ctx context.Context, prompt string, tr *memory.Trace) Result {
	// existing degrade() body, returning Result instead of agent.Response
}
```

Methods `tryPrimary` and `tryFallback` become private methods of Orchestrator.

Metrics calls (`requestsTotal.WithLabelValues(...)` etc.) — these are package-level `prometheus.Counter` defined in `telemetry/metrics.go`. Two options:
- (a) Export a thin getter from telemetry like `telemetry.RecordTier(tier, dur)` and call from orchestrator.
- (b) Make collectors package-level in telemetry and access via `telemetry.RequestsTotal.WithLabelValues(...)`.

Pick (b) — minimal churn. Ensure the global vars are exported in `telemetry/metrics.go`.

- [ ] **Step 3: Delete migrated code from agent/agent.go**

Remove `degrade`, `tryPrimary`, `tryFallback`. `Agent.ask` becomes a delegate that just calls into Pipeline (next task).

For now (interim state), have `Agent.ask` call `o.Degrade()` directly — Pipeline migration is the next step.

- [ ] **Step 4: Build + test**

```bash
go build ./... && go test -race -count=1 ./...
```
A lot of churn. Fix everything until green.

- [ ] **Step 5: Commit**

```bash
git add agent/ orchestrator/
git commit -m "refactor(orchestrator): move degradation chain (degrade/tryPrimary/tryFallback)"
```

## Task F3b.4: Move loadshed+bulkhead wrapping into Pipeline

**Files:**
- Create: `orchestrator/pipeline.go`
- Modify: `agent/agent.go`

- [ ] **Step 1: Implement Pipeline**

`orchestrator/pipeline.go`:
```go
package orchestrator

import (
	"context"
	"errors"
	"log/slog"

	"github.com/yabanci/flowguard/bulkhead"
	"github.com/yabanci/flowguard/loadshed"

	"github.com/yabanci/agentshield/config"
	"github.com/yabanci/agentshield/memory"
	"github.com/yabanci/agentshield/telemetry"
)

// Pipeline wraps Orchestrator with loadshed → bulkhead, and exposes the public
// Do/Stream entry points used by Agent.
type Pipeline struct {
	cfg         *config.Config
	log         *slog.Logger
	shedder     *loadshed.Shedder
	interactive *bulkhead.Bulkhead
	batch       *bulkhead.Bulkhead
	orch        *Orchestrator
	tel         *telemetry.Store
	traces      *memory.TraceStore
}

func NewPipeline(cfg *config.Config, orch *Orchestrator, tel *telemetry.Store,
	traces *memory.TraceStore, log *slog.Logger) *Pipeline {
	return &Pipeline{
		cfg:         cfg,
		log:         log,
		shedder:     loadshed.New(cfg.Limits.LoadshedStart, cfg.Limits.LoadshedWindow),
		interactive: bulkhead.New(cfg.Limits.InteractiveSlots, bulkhead.WithMaxWait(2*time.Second)),
		batch:       bulkhead.New(cfg.Limits.BatchSlots, bulkhead.WithMaxWait(0)),
		orch:        orch,
		tel:         tel,
		traces:      traces,
	}
}

func (p *Pipeline) Do(ctx context.Context, prompt string, batch bool) (Result, *memory.Trace, error) {
	tr := p.traces.New(prompt)

	var result Result
	err := p.shedder.Do(ctx, func(ctx context.Context) error {
		bh := p.interactive
		if batch {
			bh = p.batch
		}
		return bh.Do(ctx, func(ctx context.Context) error {
			result = p.orch.Degrade(ctx, prompt, tr)
			return nil
		})
	})

	if errors.Is(err, loadshed.ErrShed) {
		result = Result{Text: "Server is overloaded. Please try again in a moment.", Tier: TierDegraded}
		tr.AddStep(memory.TraceStep{Tier: memory.Tier(TierDegraded), Outcome: memory.OutcomeGracefulDenial})
		p.tel.Costs.Record(memory.Tier(TierDegraded), prompt, "")
		// telemetry.LoadshedTotal.Inc()
	} else if errors.Is(err, bulkhead.ErrFull) {
		result = Result{Text: "Too many concurrent requests. Please try again shortly.", Tier: TierDegraded}
		tr.AddStep(memory.TraceStep{Tier: memory.Tier(TierDegraded), Outcome: memory.OutcomeGracefulDenial})
		p.tel.Costs.Record(memory.Tier(TierDegraded), prompt, "")
	} else if err != nil {
		return result, tr, err
	}

	tr.Finalize(memory.Tier(result.Tier))
	return result, tr, nil
}
```

Note: `memory.Tier` and `orchestrator.Tier` are the same string. Decide where it lives — recommend memory (it's used by trace/cost too). Adjust the alias accordingly. Update `costs.Record` signature if needed.

Update Pipeline accessors:
```go
func (p *Pipeline) Chaos() *Chaos                    { return p.orch.chaos }
func (p *Pipeline) Shedder() *loadshed.Shedder       { return p.shedder }
func (p *Pipeline) Interactive() *bulkhead.Bulkhead  { return p.interactive }
func (p *Pipeline) Batch() *bulkhead.Bulkhead        { return p.batch }
```

- [ ] **Step 2: Replace Agent.Ask body with Pipeline.Do delegation**

In `agent/agent.go`:
```go
func (a *Agent) Ask(ctx context.Context, prompt string) (Response, error) {
	a.totalRequests.Add(1)
	r, tr, err := a.pipeline.Do(ctx, prompt, false)
	if err != nil {
		return Response{}, err
	}
	return Response{
		Text:    r.Text,
		Tier:    Tier(r.Tier),
		Cached:  r.Cached,
		TraceID: tr.ID,
	}, nil
}

func (a *Agent) AskBatch(ctx context.Context, prompt string) (Response, error) {
	a.totalRequests.Add(1)
	r, tr, err := a.pipeline.Do(ctx, prompt, true)
	if err != nil {
		return Response{}, err
	}
	return Response{Text: r.Text, Tier: Tier(r.Tier), Cached: r.Cached, TraceID: tr.ID}, nil
}
```

Remove the old shedder/bulkhead/orchestrator fields from `Agent` struct — Pipeline owns them now.

- [ ] **Step 3: Build + test**

```bash
go build ./... && go test -race -count=1 ./...
```

- [ ] **Step 4: Commit**

```bash
git add agent/ orchestrator/
git commit -m "refactor(orchestrator): Pipeline wraps loadshed+bulkhead — Agent.Ask delegates"
```

## Task F3b.5: Move StreamWithQualityGate to orchestrator/stream.go

**Files:**
- Create: `orchestrator/stream.go`
- Modify: `agent/agent.go`

- [ ] **Step 1: Move StreamWithQualityGate**

Cut the `StreamWithQualityGate` and `StreamToken` from `agent/agent.go` and paste into `orchestrator/stream.go`. Adapt to use `o.primary` / `o.fallback` / `o.chaos.IsPrimaryKilled()` / `o.breakers.*` / `o.cfg.Models.Primary` / `o.cfg.Models.Fallback`.

The hallucinationScore() helper that the gate calls — currently lives in `quality/`. Make sure it's exported (`quality.HallucinationScore`).

```go
package orchestrator

import (
	"context"
	"strings"

	"github.com/yabanci/agentshield/provider"
	"github.com/yabanci/agentshield/quality"
)

type StreamToken struct {
	Token    string `json:"token,omitempty"`
	Done     bool   `json:"done,omitempty"`
	Tier     Tier   `json:"tier"`
	Switched bool   `json:"switched,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func (o *Orchestrator) Stream(ctx context.Context, prompt string, out chan<- StreamToken) (Tier, error) {
	canUsePrimary := !o.chaos.IsPrimaryKilled() &&
		!o.breakers.PrimarySemantic.ShouldBlock() &&
		o.breakers.PrimaryTransport.State().String() == "closed"

	if canUsePrimary {
		streamCtx, cancelStream := context.WithCancel(ctx)
		rawTokens := make(chan string, 64)
		var streamErr error

		go func() {
			streamErr = o.primary.Stream(streamCtx, provider.Request{
				Model: o.cfg.Models.Primary, Prompt: prompt,
			}, rawTokens)
		}()

		var buf strings.Builder
		tokenCount := 0
		tripped := false

		for token := range rawTokens {
			buf.WriteString(token)
			tokenCount++
			if tokenCount%30 == 0 && tokenCount <= 120 {
				score, reason := quality.HallucinationScore(buf.String())
				if score < 0.5 {
					tripped = true
					cancelStream()
					for range rawTokens {
					}
					out <- StreamToken{Switched: true, Tier: TierFallback, Reason: "quality gate: " + reason}
					break
				}
			}
			out <- StreamToken{Token: token, Tier: TierPrimary}
		}
		cancelStream()

		if !tripped && streamErr == nil {
			return TierPrimary, nil
		}
	}

	rawTokens := make(chan string, 64)
	go func() {
		_ = o.fallback.Stream(ctx, provider.Request{Model: o.cfg.Models.Fallback, Prompt: prompt}, rawTokens)
	}()
	for token := range rawTokens {
		out <- StreamToken{Token: token, Tier: TierFallback}
	}
	return TierFallback, nil
}
```

In `agent/agent.go` add a thin delegate:
```go
type StreamToken = orchestrator.StreamToken

func (a *Agent) StreamWithQualityGate(ctx context.Context, prompt string, out chan<- StreamToken) (Tier, error) {
	t, err := a.pipeline.Stream(ctx, prompt, out)
	return Tier(t), err
}
```

Add `Pipeline.Stream` that just delegates to `Orchestrator.Stream`:
```go
func (p *Pipeline) Stream(ctx context.Context, prompt string, out chan<- StreamToken) (Tier, error) {
	return p.orch.Stream(ctx, prompt, out)
}
```

- [ ] **Step 2: Build + test**

```bash
go build ./... && go test -race -count=1 ./...
```

The TestStream_QualityGateSwitchesToFallback test from agent/ — move it to `orchestrator/stream_test.go`. While moving, replace the dependency on real Ollama with a mock LLMProvider that returns a controlled token sequence (this addresses the "engineered test" concern from NEXT_STEPS.md). Sketch:

```go
type tokenScript struct{ tokens []string }
func (s tokenScript) Stream(ctx context.Context, req provider.Request, out chan<- string) error {
	defer close(out)
	for _, t := range s.tokens {
		out <- t
	}
	return nil
}
// implement other LLMProvider methods as no-ops
```

Test: feed tokens that build up a hallucination marker after 30 tokens, assert switch.

- [ ] **Step 3: Commit**

```bash
git add agent/ orchestrator/
git commit -m "refactor(orchestrator): move StreamWithQualityGate + reseat test on mock LLMProvider"
```

## Task F3b.6: Move chaos accessors (Kill/Restore/Enable) — Agent thin facade

**Files:**
- Modify: `agent/agent.go`

- [ ] **Step 1: Replace chaos methods with delegates**

In `agent/agent.go`:
```go
func (a *Agent) KillPrimary()         { a.pipeline.Chaos().KillPrimary() }
func (a *Agent) RestorePrimary()      { a.pipeline.Chaos().RestorePrimary() }
func (a *Agent) KillFallback()        { a.pipeline.Chaos().KillFallback() }
func (a *Agent) RestoreFallback()     { a.pipeline.Chaos().RestoreFallback() }
func (a *Agent) EnableDegradeMode()   { a.pipeline.Chaos().EnableDegrade() }
func (a *Agent) DisableDegradeMode()  { a.pipeline.Chaos().DisableDegrade() }
```

Move the existing chaos scenario runner (`StartChaos`, `RunChaos`) to `orchestrator/chaos_runner.go` (same package as Chaos). Agent gets a delegate:
```go
func (a *Agent) StartChaos(ctx context.Context) (<-chan orchestrator.ChaosEvent, error) {
	return a.pipeline.Chaos().StartScenario(ctx, a.pipeline)
}
```

Adjust ChaosEvent type re-export:
```go
type ChaosEvent = orchestrator.ChaosEvent
```

- [ ] **Step 2: Verify final Agent shape**

`agent/agent.go` `Agent` struct should now contain only:
```go
type Agent struct {
	cfg           *config.Config
	log           *slog.Logger
	pipeline      *orchestrator.Pipeline
	memory        *memory.Store
	telemetry     *telemetry.Store
	totalRequests atomic.Int64
	lifeCtx       context.Context
	lifeCancel    context.CancelFunc
}
```

That's 8 fields. Acceptable (≤ 7 was a stretch goal — `totalRequests` belongs naturally in telemetry; consider moving in a follow-up small commit if it fits cleanly without adding a getter chain).

- [ ] **Step 3: Build + test + lint**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```

- [ ] **Step 4: Commit**

```bash
git add agent/ orchestrator/
git commit -m "refactor(agent): facade — delegate chaos to pipeline.Chaos()"
```

## Task F3b.7: Status() pulls from telemetry.Store

**Files:**
- Modify: `agent/agent.go`

- [ ] **Step 1: Refactor Status**

```go
func (a *Agent) Status() Status {
	primaryState := a.pipeline.Orchestrator().Breakers().PrimaryTransport.State().String()
	if a.pipeline.Chaos().IsPrimaryKilled() {
		primaryState = "killed"
	}
	fallbackState := a.pipeline.Orchestrator().Breakers().FallbackTransport.State().String()
	if a.pipeline.Chaos().IsFallbackKilled() {
		fallbackState = "killed"
	}
	pSem := a.pipeline.Orchestrator().Breakers().PrimarySemantic.Snapshot()
	fSem := a.pipeline.Orchestrator().Breakers().FallbackSemantic.Snapshot()

	pr, fr, cr, dr := a.telemetry.Costs.TierCounts()
	tierCounts := telemetry.TierRequestCounts{Primary: pr, Fallback: fr, Cache: cr, Denied: dr}

	scoreIn := telemetry.ScoreInput{
		PrimaryBreaker:     primaryState,
		FallbackBreaker:    fallbackState,
		PrimaryKilled:      a.pipeline.Chaos().IsPrimaryKilled(),
		FallbackKilled:     a.pipeline.Chaos().IsFallbackKilled(),
		PrimarySemanticCB:  pSem,
		FallbackSemanticCB: fSem,
		TierCounts:         tierCounts,
		Latency:            a.telemetry.Latency.Snapshot(),
	}
	score := a.telemetry.ScoreFor(scoreIn)
	a.memory.ScoreHistory.Record(score.Total)

	return Status{
		PrimaryBreaker:     primaryState,
		FallbackBreaker:    fallbackState,
		PrimaryKilled:      a.pipeline.Chaos().IsPrimaryKilled(),
		FallbackKilled:     a.pipeline.Chaos().IsFallbackKilled(),
		CacheSize:          a.pipeline.Orchestrator().Cache().Size(),
		TotalRequests:      a.totalRequests.Load(),
		ErrorRate:          a.pipeline.Orchestrator().Breakers().PrimaryTransport.ErrorRate(),
		LoadshedLimit:      a.pipeline.Shedder().CurrentLimit(),
		LoadshedInflight:   a.pipeline.Shedder().Inflight(),
		InteractiveBusy:    a.pipeline.Interactive().ActiveCount(),
		BatchBusy:          a.pipeline.Batch().ActiveCount(),
		ActiveSessions:     a.memory.Sessions.Count(),
		ChaosRunning:       a.pipeline.Chaos().IsRunning(),
		PrimarySemanticCB:  pSem,
		FallbackSemanticCB: fSem,
		DegradeMode:        a.pipeline.Chaos().IsDegradeEnabled(),
		Costs:              a.telemetry.Costs.Snapshot(),
		TierCounts:         tierCounts,
		Latency:            a.telemetry.Latency.Snapshot(),
		Score:              score,
	}
}
```

Add accessors to Pipeline / Orchestrator as referenced (`Orchestrator()`, `Breakers()`, `Cache()`).

- [ ] **Step 2: Build + test + lint**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```

- [ ] **Step 3: Commit**

```bash
git add agent/ orchestrator/
git commit -m "refactor(agent): Status() pulls snapshots from pipeline + telemetry stores"
```

## F3b — End-of-phase checkpoint

- [ ] **Verify final Agent field count ≤ 8**

```bash
awk '/^type Agent struct/,/^}$/' agent/agent.go | grep -c $'\t'
```
Expected: ≤ 8.

- [ ] **Run full pipeline + smoke test**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
go build -o /tmp/agentshield-f3b .
PORT=8082 OLLAMA_URL=http://localhost:11434 /tmp/agentshield-f3b &
sleep 1
curl -s http://localhost:8082/status | head -c 500
kill %1
```
The status endpoint should return a JSON with all the same fields as before (judges' dashboard depends on it).

- [ ] **Push + open PR + wait green + merge**

```bash
git push -u origin feat/foundation-f3b-orchestrator
gh pr create --title "refactor(foundation/F3b): extract orchestrator/pipeline/chaos — Agent is a facade" \
  --body "Foundation MR 4 of 6. Agent struct: 27 → 8 fields. Degradation chain owned by Orchestrator. Loadshed+bulkhead owned by Pipeline. Chaos owns kill/degrade flags."
```

After merge: `git checkout main && git pull`.

---

# PHASE F4 — dashboard → embed.FS

**Branch:** `feat/foundation-f4-dashboard`
**Outcome:** Zero HTML/CSS/JS literals in `.go` files. `prettier` and `stylelint` can run on the assets.

## Task F4.0: Branch

```bash
git checkout main && git pull
git checkout -b feat/foundation-f4-dashboard
```

## Task F4.1: Extract dashboard literal into separate files

**Files:**
- Read: `agent/dashboard.go` (locate it — may live in `api/dashboard.go` after F1; verify with `grep -l dashboardHTML *.go agent/*.go api/*.go`)
- Create: `api/web/templates/dashboard.html.tmpl`
- Create: `api/web/static/dashboard.css`
- Create: `api/web/static/dashboard.js`
- Create: `api/web/embed.go`
- Modify: handler that serves `/`
- Delete: `dashboard.go`

- [ ] **Step 1: Locate the existing literal**

```bash
grep -l "dashboardHTML\|<!DOCTYPE html\|<html" --include="*.go" -r .
```

Open the file and identify:
- The `<style>...</style>` block → goes to `api/web/static/dashboard.css` (without the `<style>` tags)
- The `<script>...</script>` block → goes to `api/web/static/dashboard.js`
- Everything else (HTML body, templates, etc.) → goes to `api/web/templates/dashboard.html.tmpl`

Replace the inline `<style>` tag with `<link rel="stylesheet" href="/static/dashboard.css">`.
Replace the inline `<script>` tag with `<script src="/static/dashboard.js"></script>`.

- [ ] **Step 2: Create directory layout**

```bash
mkdir -p api/web/templates api/web/static
```

- [ ] **Step 3: Write embed.go**

`api/web/embed.go`:
```go
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates static
var fsys embed.FS

// Templates returns the parsed dashboard template set.
func Templates() *template.Template {
	return template.Must(template.ParseFS(fsys, "templates/*.tmpl"))
}

// StaticHandler serves files from the embedded static/ directory.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(fsys, "static")
	if err != nil {
		panic(err) // build-time invariant
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
```

- [ ] **Step 4: Update handler to render template + serve /static/**

In `api/handler.go` Register:
```go
mux.HandleFunc("GET /", h.dashboard)
mux.Handle("GET /static/", web.StaticHandler())
```

Replace dashboard handler:
```go
import "github.com/yabanci/agentshield/api/web"

var dashboardTmpl = web.Templates()

func (h *Handler) dashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTmpl.ExecuteTemplate(w, "dashboard.html.tmpl", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

Delete the old `dashboard.go` / `dashboardHTML` constant.

- [ ] **Step 5: Build + test + visual smoke**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
go build -o /tmp/agentshield-f4 .
PORT=8084 OLLAMA_URL=http://localhost:11434 /tmp/agentshield-f4 &
sleep 1
curl -s http://localhost:8084/ | head -c 500
curl -s http://localhost:8084/static/dashboard.css | head -c 200
curl -s http://localhost:8084/static/dashboard.js | head -c 200
kill %1
```
Expected: HTML page contains `<link rel="stylesheet"...>`, CSS and JS endpoints serve content.

If you have a browser available, open `http://localhost:8084/` and confirm the page looks identical to before (no broken styling).

- [ ] **Step 6: Verify invariant — zero HTML literals in .go**

```bash
grep -l "<!DOCTYPE html\|<style>\|<script>" --include="*.go" -r .
```
Expected: empty (or only `api/web/embed.go` if your grep flags include the tag in a comment — which it shouldn't).

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor(api): dashboard → templates/static + embed.FS"
```

## F4 — End-of-phase checkpoint

- [ ] **Push + PR + merge**

```bash
git push -u origin feat/foundation-f4-dashboard
gh pr create --title "refactor(foundation/F4): dashboard → embed.FS — kill the 900-line literal" \
  --body "Foundation MR 5 of 6. HTML/CSS/JS extracted from Go literal into api/web/. Dev/prod modes are bit-for-bit identical."
```

Wait green, merge, `git checkout main && git pull`.

---

# PHASE F5 — slog through agent/ + api/

**Branch:** `feat/foundation-f5-slog`
**Outcome:** Every component takes `*slog.Logger` in its constructor. Standard log keys defined as constants. PII guard via forbidigo.

## Task F5.0: Branch

```bash
git checkout main && git pull
git checkout -b feat/foundation-f5-slog
```

## Task F5.1: Standard log keys constants

**Files:**
- Create: `internal/logkeys/keys.go`

- [ ] **Step 1: Define keys**

```bash
mkdir -p internal/logkeys
```

`internal/logkeys/keys.go`:
```go
// Package logkeys defines stable structured-log field names. Dashboards and
// alerts in HyperDX/Loki/CloudWatch key off these — never inline a literal
// string in a log call when a constant exists here.
package logkeys

const (
	Component    = "component"
	TraceID      = "trace_id"
	SessionID    = "session_id"
	Tier         = "tier"
	Model        = "model"
	Outcome      = "outcome"
	LatencyMS    = "latency_ms"
	QualityScore = "quality_score"
	CBState      = "cb_state"
	Err          = "err"
)
```

- [ ] **Step 2: Commit**

```bash
git add internal/logkeys/keys.go
git commit -m "feat(logkeys): standard structured-log field constants"
```

## Task F5.2: LoggerFromContext helpers

**Files:**
- Create: `internal/logctx/logctx.go`
- Create: `internal/logctx/logctx_test.go`

- [ ] **Step 1: Write failing test**

`internal/logctx/logctx_test.go`:
```go
package logctx_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/yabanci/agentshield/internal/logctx"
)

func TestRoundTrip(t *testing.T) {
	base := slog.Default()
	want := base.With("k", "v")
	ctx := logctx.With(context.Background(), want)
	got := logctx.From(ctx)
	if got != want {
		t.Errorf("From returned wrong logger")
	}
}

func TestFromMissingReturnsDefault(t *testing.T) {
	got := logctx.From(context.Background())
	if got == nil {
		t.Errorf("From returned nil; want default")
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/logctx/...
```
Expected: build error.

- [ ] **Step 3: Implement**

`internal/logctx/logctx.go`:
```go
// Package logctx attaches a *slog.Logger to a context.Context.
// Use With at request entry to bind trace_id; use From inside any function on
// the request path to retrieve the contextualised logger.
package logctx

import (
	"context"
	"log/slog"
)

type ctxKey struct{}

func With(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, log)
}

func From(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(ctxKey{}).(*slog.Logger); ok {
		return log
	}
	return slog.Default()
}
```

- [ ] **Step 4: Verify pass**

```bash
go test ./internal/logctx/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/logctx/
git commit -m "feat(logctx): LoggerFromContext helper"
```

## Task F5.3: Inject *slog.Logger through constructors

**Files:**
- Modify: `agent/agent.go`, `orchestrator/*.go`, `memory/store.go`, `telemetry/store.go`, `provider/ollama.go`, `cache/semantic.go`, `quality/evaluator.go`

- [ ] **Step 1: Add Logger to Config + main.go logger init**

`config/config.go` — already has `LoggerConfig`. Add:
```go
import "log/slog"

func (c *Config) NewLogger(out io.Writer) *slog.Logger {
	var h slog.Handler
	switch c.Logger.Format {
	case "json":
		h = slog.NewJSONHandler(out, &slog.HandlerOptions{Level: c.Logger.Level})
	default:
		h = slog.NewTextHandler(out, &slog.HandlerOptions{Level: c.Logger.Level})
	}
	return slog.New(h)
}
```

`config/env.go` — read `LOG_LEVEL` and `LOG_FORMAT`:
```go
if v := os.Getenv("LOG_LEVEL"); v != "" {
	switch strings.ToLower(v) {
	case "debug": c.Logger.Level = slog.LevelDebug
	case "info":  c.Logger.Level = slog.LevelInfo
	case "warn":  c.Logger.Level = slog.LevelWarn
	case "error": c.Logger.Level = slog.LevelError
	}
}
if v := os.Getenv("LOG_FORMAT"); v == "json" || v == "text" {
	c.Logger.Format = v
}
```

`main.go`:
```go
cfg, err := config.LoadFromEnv()
if err != nil { /* ... */ }
logger := cfg.NewLogger(os.Stdout)

a := agent.NewWithConfig(cfg, logger)
```

- [ ] **Step 2: Add logger param to every component constructor**

Walk through each:
- `agent.NewWithConfig(cfg)` → `agent.NewWithConfig(cfg, log)` — store `a.log = log`
- `orchestrator.New(...)` already has `log`
- `orchestrator.NewPipeline(...)` already has `log`
- `provider.NewOllama(cfg)` → `provider.NewOllama(cfg, log)`
- `cache.New(ttl, embedder)` → `cache.New(ttl, embedder, log)`
- `quality.NewEvaluator(embedder)` → `quality.NewEvaluator(embedder, log)`
- `memory.NewStore(cfg, asker, log)` already has `log`
- `telemetry.NewStore(cfg, log)` already has `log`

Each component stores `c.log = log.With(slog.String(logkeys.Component, "<name>"))`.

- [ ] **Step 3: Add request-scoped logger in Pipeline.Do**

In `orchestrator/pipeline.go Pipeline.Do`:
```go
import "github.com/yabanci/agentshield/internal/logctx"
import "github.com/yabanci/agentshield/internal/logkeys"

func (p *Pipeline) Do(ctx context.Context, prompt string, batch bool) (Result, *memory.Trace, error) {
	tr := p.traces.New(prompt)
	reqLog := p.log.With(slog.String(logkeys.TraceID, tr.ID))
	ctx = logctx.With(ctx, reqLog)

	reqLog.Debug("pipeline.do", slog.Bool("batch", batch))
	// ... rest
}
```

- [ ] **Step 4: Add INFO log on every tier transition in Orchestrator**

In each tier branch of `Degrade`, add at the success/fail point:
```go
log := logctx.From(ctx)
log.Info("tier completed",
	slog.String(logkeys.Tier, string(tier)),
	slog.Int64(logkeys.LatencyMS, dur.Milliseconds()),
	slog.String(logkeys.Outcome, "success"),
)
```

For failures use Warn level. Never include `prompt` or response text in the log — only IDs and metrics.

- [ ] **Step 5: Build + test + lint**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat(slog): inject *slog.Logger through every component + request-scoped trace_id"
```

## Task F5.4: forbidigo PII guard

**Files:**
- Modify: `.golangci.yml`

- [ ] **Step 1: Add PII patterns to forbidigo**

In `.golangci.yml`, extend the forbidigo block:
```yaml
linters:
  settings:
    forbidigo:
      forbid:
        - pattern: '^os\.Getenv$'
          msg: "Read env vars in package config only."
        - pattern: '^os\.LookupEnv$'
          msg: "Read env vars in package config only."
        - pattern: '\.Info\(.*\bprompt\b'
          msg: "Do not log raw prompt — log trace_id and use a quality score field if needed."
        - pattern: '\.Info\(.*\bresponse\b'
          msg: "Do not log raw response — log tier + outcome instead."
        - pattern: '\.Debug\(.*\bprompt\b'
          msg: "Do not log raw prompt at any level."
```

Note: `forbidigo` matches Go expressions, not source-code substrings, so the third/fourth patterns above are best-effort. They will catch `log.Info("prompt: " + prompt)` style mistakes but not all variants. Pair with code review.

- [ ] **Step 2: Run lint, fix any catches in existing code**

```bash
golangci-lint run --timeout=3m
```

- [ ] **Step 3: Commit**

```bash
git add .golangci.yml
git commit -m "chore(lint): forbidigo PII guard — ban prompt/response in log calls"
```

## F5 — End-of-phase checkpoint

- [ ] **Run full pipeline + smoke test JSON logging**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
go build -o /tmp/agentshield-f5 .
PORT=8085 LOG_FORMAT=json OLLAMA_URL=http://localhost:11434 /tmp/agentshield-f5 &
sleep 1
curl -s http://localhost:8085/health/live
sleep 1
kill %1 2>/dev/null
# Look at the JSON-formatted startup log line — should include "level":"INFO" and structured fields.
```

- [ ] **Push + PR + merge**

```bash
git push -u origin feat/foundation-f5-slog
gh pr create --title "feat(foundation/F5): slog throughout — structured logging with trace_id propagation" \
  --body "Foundation MR 6 of 6. Logger flows through context. Standard keys in internal/logkeys. PII guarded by forbidigo. Foundation refactor complete."
```

After merge: `git checkout main && git pull`.

---

# Final acceptance gate

After all 6 MRs are merged, run from `main`:

- [ ] **Field count**

```bash
awk '/^type Agent struct/,/^}$/' agent/agent.go | grep -c $'\t'
```
Expected: ≤ 8.

- [ ] **Zero os.Getenv outside config**

```bash
grep -rn "os\.Getenv\|os\.LookupEnv" --include="*.go" | grep -v "_test.go" | grep -v "^config/"
```
Expected: empty.

- [ ] **Zero ollamaClient references**

```bash
grep -rn "ollamaClient" --include="*.go"
```
Expected: empty.

- [ ] **Zero HTML literals in .go**

```bash
grep -l "<!DOCTYPE html\|<style>\|<script>" --include="*.go" -r .
```
Expected: empty.

- [ ] **Coverage gates**

```bash
go test -coverprofile=/tmp/cov.out ./...
go tool cover -func=/tmp/cov.out | grep -E "config/|provider/" | tail -10
```
Expected: `config/` and `provider/` ≥ 95%; other new packages ≥ 80%.

- [ ] **Full build + race + lint + smoke**

```bash
go build ./... && go test -race -count=1 ./... && golangci-lint run --timeout=3m
go build -o /tmp/as-final .
PORT=8086 OLLAMA_URL=http://localhost:11434 /tmp/as-final &
sleep 1
curl -s http://localhost:8086/status > /tmp/status.json
curl -s http://localhost:8086/ | grep -q '<title>'
kill %1
diff <(jq 'keys' /tmp/status.json) <(echo '["active_sessions","batch_busy","cache_size","chaos_running","costs","degrade_mode","error_rate","fallback_breaker","fallback_killed","fallback_semantic_cb","interactive_busy","latency","loadshed_inflight","loadshed_limit","primary_breaker","primary_killed","primary_semantic_cb","score","tier_counts","total_requests"]')
```
Expected: status JSON keys identical to before — judges' dashboard untouched.

Foundation refactor done. Next: brainstorm next sub-project (multi-provider impl / OTEL exporter / load test / etc.) — each one its own spec → plan cycle.
