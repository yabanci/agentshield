package api

import (
	"strings"
	"testing"

	"github.com/yabanci/agentshield/config"
)

// cfg helpers reduce noise across cases.
func cfgDefault() *config.Config { return config.Defaults() }
func cfgAllowHTTP() *config.Config {
	c := config.Defaults()
	c.Webhook.AllowHTTP = true
	return c
}
func cfgAllowPrivate() *config.Config {
	c := config.Defaults()
	c.Webhook.AllowPrivate = true
	c.Webhook.AllowHTTP = true
	return c
}

func TestWebhookValidate_AllowsHTTPS(t *testing.T) {
	if err := validateWebhookURL("https://example.com/hook", cfgDefault()); err != nil {
		t.Errorf("https://example.com should be allowed, got: %v", err)
	}
}

func TestWebhookValidate_RejectsHTTPByDefault(t *testing.T) {
	err := validateWebhookURL("http://example.com/hook", cfgDefault())
	if err == nil {
		t.Error("http:// should be rejected without Webhook.AllowHTTP")
	}
}

func TestWebhookValidate_AllowsHTTPWhenEnabled(t *testing.T) {
	if err := validateWebhookURL("http://example.com/hook", cfgAllowHTTP()); err != nil {
		t.Errorf("http:// should be allowed with cfg override, got: %v", err)
	}
}

func TestWebhookValidate_RejectsPrivateIPs(t *testing.T) {
	cases := []string{
		"https://10.0.0.1/hook",             // private RFC1918
		"https://192.168.1.1/hook",          // private RFC1918
		"https://172.16.0.1/hook",           // private RFC1918
		"https://127.0.0.1/hook",            // loopback
		"https://localhost/hook",            // loopback hostname
		"https://0.0.0.0/hook",              // unspecified
		"https://169.254.0.1/hook",          // link-local
		"https://host.docker.internal/hook", // explicit docker
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			if err := validateWebhookURL(u, cfgDefault()); err == nil {
				t.Errorf("%s should be rejected as private", u)
			}
		})
	}
}

func TestWebhookValidate_AllowsPrivateWhenEnabled(t *testing.T) {
	if err := validateWebhookURL("http://127.0.0.1:8080/hook", cfgAllowPrivate()); err != nil {
		t.Errorf("private IP should be allowed in dev mode, got: %v", err)
	}
}

func TestWebhookValidate_RejectsBadSchemes(t *testing.T) {
	cases := []string{
		"file:///etc/passwd",
		"gopher://attacker.com",
		"ftp://internal-server",
		"javascript:alert(1)",
		"data:text/html,<script>",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			if err := validateWebhookURL(u, cfgDefault()); err == nil {
				t.Errorf("%s should be rejected (bad scheme)", u)
			}
		})
	}
}

func TestWebhookValidate_RejectsMalformed(t *testing.T) {
	if err := validateWebhookURL("", cfgDefault()); err == nil {
		t.Error("empty URL should be rejected")
	}
	// "://no-scheme" parses without error in Go but has empty scheme — should reject.
	err := validateWebhookURL("://no-scheme", cfgDefault())
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Errorf("URL without scheme should fail with scheme error, got: %v", err)
	}
}

func TestWebhookValidate_RejectsMissingHost(t *testing.T) {
	err := validateWebhookURL("http:///path", cfgAllowHTTP())
	if err == nil {
		t.Error("URL without host should be rejected")
	}
}

func TestWebhookValidate_AcceptsPublicHostnames(t *testing.T) {
	cases := []string{
		"https://hooks.slack.com/services/T00/B00/xxx",
		"https://discord.com/api/webhooks/123/abc",
		"https://example.com/callback",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			if err := validateWebhookURL(u, cfgDefault()); err != nil {
				t.Errorf("public hostname should be allowed: %v", err)
			}
		})
	}
}
