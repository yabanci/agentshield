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

func TestOllamaProvider_Embed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3]}`))
	}))
	defer srv.Close()

	p := provider.NewOllama(config.ProviderConfig{Kind: "ollama", BaseURL: srv.URL, Timeout: 5 * time.Second})
	v, err := p.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(v) != 3 || v[1] != 0.2 {
		t.Errorf("Embed = %v, want [0.1 0.2 0.3]", v)
	}
}

func TestOllamaProvider_PingChecksTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("Ping should hit /api/tags, got %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	p := provider.NewOllama(config.ProviderConfig{Kind: "ollama", BaseURL: srv.URL, Timeout: time.Second})
	if err := p.Ping(context.Background()); err != nil {
		t.Errorf("Ping: %v", err)
	}
}
