package agent

// MCPLookupTool is a 5th built-in tool that calls an external
// MCP-compatible (Model Context Protocol) server over HTTP. It's the
// concrete answer to the TrueFoundry challenge brief's third failure
// mode — "what if an MCP server starts erroring out?" — wired through
// the existing per-tool circuit breaker so AgentShield's resilience
// stack covers it without any new infrastructure.
//
// The tool talks to whatever MCP_URL points at. For the live demo we
// ship a tiny mock at cmd/mcp-mock/ that exposes /mcp/call along with
// /mcp/kill + /mcp/restore endpoints so the presenter can show the tool
// CB tripping in real time. Production users point MCP_URL at their
// real MCP server (Anthropic's reference impl, fastmcp, etc).
//
// Protocol (intentionally minimal — we don't carry the full MCP spec
// in a hackathon demo):
//
//   POST {MCP_URL}/mcp/call
//   { "tool": "<tool name>", "args": "<freeform arg string>" }
//   200 OK { "result": "<text>" }
//   503    when the mock is in killed state
//
// CB integration: ToolRegistry.register wraps every tool — including
// this one — in a flowguard circuitbreaker (3 failures → open for 20s).
// During a demo: hit /mcp/kill → call this tool ~3 times → tool CB
// opens → next invocations get the CB rejection without hitting the
// dead MCP server → restore → CB half-opens → recovery.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type MCPLookupTool struct {
	url    string
	client *http.Client
}

// NewMCPLookupTool returns the tool only if mcpURL is non-empty. Empty
// URL = MCP integration disabled; the registry simply doesn't register
// the tool and the ReAct system prompt omits it. This keeps the default
// (no MCP) experience identical to before so existing tests don't break.
func NewMCPLookupTool(mcpURL string) *MCPLookupTool {
	return &MCPLookupTool{
		url: mcpURL,
		// Aggressive timeout — MCP calls should be sub-second; if they
		// drag past 5s the tool CB will trip and ReAct loops won't stall.
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (t *MCPLookupTool) Name() string { return "mcp_lookup" }
func (t *MCPLookupTool) Description() string {
	return "Call an external MCP server tool. Use for weather, currency, " +
		"or any data the local tools don't cover. Routed through a circuit " +
		"breaker so MCP outages degrade gracefully."
}
func (t *MCPLookupTool) ArgsSchema() string {
	return `{"tool": "weather", "args": "Berlin"}`
}

func (t *MCPLookupTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t.url == "" {
		return "", fmt.Errorf("MCP not configured (MCP_URL unset)")
	}
	tool, _ := args["tool"].(string)
	if tool == "" {
		return "", fmt.Errorf("mcp_lookup requires {tool:...}")
	}
	rawArgs, _ := args["args"].(string)

	body, _ := json.Marshal(map[string]string{"tool": tool, "args": rawArgs})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url+"/mcp/call", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("mcp request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("mcp call: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		// Non-200 counts as a failure for the per-tool CB. After 3 in a
		// row the breaker opens and subsequent calls reject without
		// hitting the dead server.
		return "", fmt.Errorf("mcp status %d", resp.StatusCode)
	}

	var out struct {
		Result string `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("mcp decode: %w", err)
	}
	return out.Result, nil
}
