package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yabanci/agentshield/config"
)

// mockOpenAIServer stands in for OpenAI's /v1/chat/completions and
// /v1/embeddings. It validates the request shape and returns the canned
// response we ask for. Keeping the mock tight is the whole point — if
// OpenAIProvider drifts away from the real wire format these tests fail.
type mockOpenAIServer struct {
	t              *testing.T
	wantModel      string
	wantPrompt     string
	chatBody       string
	chatStatus     int
	streamChunks   []string
	embedVec       []float64
	embedStatus    int
	gotAPIKey      string
	gotContentType string
}

func (m *mockOpenAIServer) handler(w http.ResponseWriter, r *http.Request) {
	m.gotAPIKey = r.Header.Get("Authorization")
	m.gotContentType = r.Header.Get("Content-Type")

	switch r.URL.Path {
	case "/chat/completions":
		var req openAIChatRequest
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if m.wantModel != "" && req.Model != m.wantModel {
			m.t.Errorf("model = %q, want %q", req.Model, m.wantModel)
		}
		// Locate the user message (system message may precede it).
		var userMsg string
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				userMsg = msg.Content
			}
		}
		if m.wantPrompt != "" && userMsg != m.wantPrompt {
			m.t.Errorf("user prompt = %q, want %q", userMsg, m.wantPrompt)
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			for _, chunk := range m.streamChunks {
				_, _ = w.Write([]byte(chunk))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		if m.chatStatus != 0 && m.chatStatus != http.StatusOK {
			http.Error(w, "boom", m.chatStatus)
			return
		}
		_, _ = w.Write([]byte(m.chatBody))
	case "/embeddings":
		if m.embedStatus != 0 && m.embedStatus != http.StatusOK {
			http.Error(w, "boom", m.embedStatus)
			return
		}
		resp := openAIEmbedResponse{Data: []struct {
			Embedding []float64 `json:"embedding"`
		}{{Embedding: m.embedVec}}}
		_ = json.NewEncoder(w).Encode(resp)
	case "/models":
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	default:
		http.NotFound(w, r)
	}
}

func newMock(t *testing.T) (*mockOpenAIServer, *httptest.Server) {
	t.Helper()
	m := &mockOpenAIServer{t: t}
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	t.Cleanup(srv.Close)
	return m, srv
}

func TestOpenAI_Generate_Success(t *testing.T) {
	m, srv := newMock(t)
	m.wantModel = "gpt-4o-mini"
	m.wantPrompt = "what is go?"
	m.chatBody = `{
		"choices":[{"message":{"role":"assistant","content":"Go is a language."},
		"finish_reason":"stop"}],
		"usage":{"prompt_tokens":3,"completion_tokens":5}
	}`

	p := NewOpenAI(config.ProviderConfig{
		Kind: "openai", BaseURL: srv.URL, APIKey: "sk-test", Timeout: 2 * time.Second,
	})
	resp, err := p.Generate(context.Background(), Request{
		Model: "gpt-4o-mini", Prompt: "what is go?",
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if resp.Text != "Go is a language." {
		t.Errorf("text = %q", resp.Text)
	}
	if resp.InputTokens != 3 || resp.OutputTokens != 5 {
		t.Errorf("tokens = %d/%d", resp.InputTokens, resp.OutputTokens)
	}
	if m.gotAPIKey != "Bearer sk-test" {
		t.Errorf("auth header = %q", m.gotAPIKey)
	}
}

func TestOpenAI_Generate_PropagatesUpstreamStatus(t *testing.T) {
	_, srv := newMock(t)
	// No chatBody set — handler will route through default chat handler
	// but with status injection.
	m, _ := newMock(t)
	_ = m
	override := &mockOpenAIServer{t: t, chatStatus: http.StatusUnauthorized}
	overrideSrv := httptest.NewServer(http.HandlerFunc(override.handler))
	defer overrideSrv.Close()
	_ = srv

	p := NewOpenAI(config.ProviderConfig{Kind: "openai", BaseURL: overrideSrv.URL})
	_, err := p.Generate(context.Background(), Request{Model: "gpt-4o", Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}

func TestOpenAI_Stream_ParsesSSEDeltas(t *testing.T) {
	m, srv := newMock(t)
	m.wantModel = "gpt-4o-mini"
	// Three OpenAI-style streaming chunks, then [DONE] is appended by the mock.
	m.streamChunks = []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo \"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\n",
	}

	p := NewOpenAI(config.ProviderConfig{Kind: "openai", BaseURL: srv.URL, APIKey: "sk-test"})
	out := make(chan string, 16)
	err := p.Stream(context.Background(), Request{Model: "gpt-4o-mini", Prompt: "hi"}, out)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	var got []string
	for s := range out {
		got = append(got, s)
	}
	joined := strings.Join(got, "")
	if joined != "hello world" {
		t.Errorf("joined deltas = %q, want %q", joined, "hello world")
	}
}

func TestOpenAI_Embed_WithoutModel_ReturnsSentinel(t *testing.T) {
	_, srv := newMock(t)
	// No EmbedModel — Embed should short-circuit without hitting the wire.
	p := NewOpenAI(config.ProviderConfig{Kind: "openai", BaseURL: srv.URL})
	_, err := p.Embed(context.Background(), "anything")
	if !errors.Is(err, ErrEmbedNotSupported) {
		t.Fatalf("expected ErrEmbedNotSupported, got %v", err)
	}
}

func TestOpenAI_Embed_WithModel(t *testing.T) {
	m, srv := newMock(t)
	m.embedVec = []float64{0.1, 0.2, 0.3}
	p := NewOpenAI(config.ProviderConfig{
		Kind: "openai", BaseURL: srv.URL, APIKey: "sk-test",
		EmbedModel: "text-embedding-3-small",
	})
	vec, err := p.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vec) != 3 || vec[0] != 0.1 {
		t.Errorf("embedding wrong: %v", vec)
	}
}

func TestOpenAI_Ping_HitsModelsEndpoint(t *testing.T) {
	_, srv := newMock(t)
	p := NewOpenAI(config.ProviderConfig{Kind: "openai", BaseURL: srv.URL})
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestOpenAI_Name_UsesKind(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"openai", "openai"},
		{"groq", "groq"},
		{"openrouter", "openrouter"},
		{"", "openai"}, // empty kind defaults to "openai"
	}
	for _, tc := range cases {
		p := NewOpenAI(config.ProviderConfig{Kind: tc.kind, BaseURL: "http://x"})
		if got := p.Name(); got != tc.want {
			t.Errorf("Name() = %q for kind=%q, want %q", got, tc.kind, tc.want)
		}
	}
}
