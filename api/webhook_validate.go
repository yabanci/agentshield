package api

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/yabanci/agentshield/config"
)

// validateWebhookURL guards against SSRF when an unprivileged user can set
// the webhook destination. Rejects:
//   - non-http(s) schemes (file://, gopher://, etc.)
//   - http:// unless cfg.Webhook.AllowHTTP is true
//   - private/loopback IPs unless cfg.Webhook.AllowPrivate is true
//   - hosts that fail to parse
func validateWebhookURL(raw string, cfg *config.Config) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("unsupported scheme %q (must be http or https)", u.Scheme)
	}
	if u.Scheme == "http" && !cfg.Webhook.AllowHTTP {
		return fmt.Errorf("http:// webhooks require AGENTSHIELD_ALLOW_HTTP_WEBHOOK=true")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("missing host")
	}

	if cfg.Webhook.AllowPrivate {
		return nil
	}
	if isLoopbackOrPrivate(host) {
		return fmt.Errorf("private/loopback hosts are not allowed (set AGENTSHIELD_ALLOW_PRIVATE_WEBHOOK=true to override)")
	}
	return nil
}

func isLoopbackOrPrivate(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || host == "host.docker.internal" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// Hostname; we can't resolve safely without leaking via DNS.
		// For a stricter check, callers should resolve and re-validate.
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}
