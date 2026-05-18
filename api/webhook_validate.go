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
//   - hostnames that resolve to any private/loopback IP at validation time
//   - hosts that fail to parse
//
// DNS rebinding caveat: a bypass remains where a hostname resolves to a
// public IP at validation time and then to a private IP when the webhook
// fires. The HTTP dispatcher must apply the same isLoopbackOrPrivateIP
// check on the resolved IP at request time to fully close that gap.
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

// isLoopbackOrPrivate accepts either an IP literal or a hostname. For
// hostnames it performs a DNS lookup and rejects if any resolved address
// is in a private/loopback/link-local/unspecified range. The pre-fix
// version returned false for any hostname, allowing trivial SSRF: register
// attacker.example.com pointing at 169.254.169.254 (cloud IMDS) and the
// validator passes.
func isLoopbackOrPrivate(host string) bool {
	host = strings.ToLower(host)
	if host == "localhost" || host == "host.docker.internal" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ipIsLoopbackOrPrivate(ip)
	}
	// Hostname — resolve and reject if ANY answer is private.
	ips, err := net.LookupIP(host)
	if err != nil {
		// Resolution failure: be defensive and reject. A webhook that can't
		// resolve at validation time isn't useful anyway, and accepting it
		// would only paper over the bypass.
		return true
	}
	for _, ip := range ips {
		if ipIsLoopbackOrPrivate(ip) {
			return true
		}
	}
	return false
}

func ipIsLoopbackOrPrivate(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsMulticast()
}
