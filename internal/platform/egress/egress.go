// Package egress provides the data plane's outbound HTTP posture: it blocks
// SSRF to internal address ranges, bounds redirect chains and response sizes,
// and validates egress targets. It is a platform capability shared by the
// model, MCP, and A2A connectors.
package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Policy is the egress posture for outbound connector traffic.
type Policy struct {
	// DisableProxy requires direct guarded dialing, ignoring proxy environment variables.
	DisableProxy bool
	// AllowLoopback permits loopback targets (local development providers).
	// All other private/internal ranges are always denied.
	AllowLoopback bool
	// AllowCIDRs (operator opt-in for self-hosted clusters) lists private or
	// internal networks that are reachable despite the default deny list. Used
	// for Kubernetes Service endpoints (e.g. an in-cluster provider) that would
	// otherwise be blocked by SSRF guardrails.
	AllowCIDRs []*net.IPNet
	// MaxRedirects bounds the redirect chain; 0 rejects every redirect.
	MaxRedirects int
	// MaxResponseBytes caps upstream response bodies; <=0 means uncapped.
	MaxResponseBytes int64
}

// LitePolicy is the egress posture of the single-operator Lite profile:
// operator-configured endpoints are honored (including local development
// providers on loopback), while internal address ranges and metadata
// endpoints stay unreachable, redirects are rejected, and response bodies are
// capped.
func LitePolicy() Policy {
	return Policy{AllowLoopback: true, MaxRedirects: 0, MaxResponseBytes: 16 << 20}
}

// LitePolicyWithAllowedCIDRs is the Lite posture extended with operator-chosen
// internal networks (self-hosted Kubernetes Service CIDRs). The deny list still
// applies to everything outside the explicit allow list.
func LitePolicyWithAllowedCIDRs(cidrs []string) (Policy, error) {
	p := LitePolicy()
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err != nil {
			return Policy{}, fmt.Errorf("egress: invalid allowed cidr %q: %w", value, err)
		}
		p.AllowCIDRs = append(p.AllowCIDRs, network)
	}
	return p, nil
}

var deniedCIDRs = mustParseCIDRs(
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"100.64.0.0/10",  // carrier-grade NAT
	"169.254.0.0/16", // link-local, incl. cloud metadata
	"127.0.0.0/8",    // loopback (unless explicitly allowed)
	"::1/128",
	"fc00::/7",  // unique-local
	"fe80::/10", // link-local
	"::/128",    // unspecified
)

func mustParseCIDRs(values ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, cidr, err := net.ParseCIDR(value)
		if err != nil {
			panic(fmt.Sprintf("egress: bad CIDR %q: %v", value, err))
		}
		nets = append(nets, cidr)
	}
	return nets
}

func (p Policy) allowIP(ip net.IP) bool {
	if p.AllowLoopback && ip.IsLoopback() {
		return true
	}
	for _, allowed := range p.AllowCIDRs {
		if allowed.Contains(ip) {
			return true
		}
	}
	for _, cidr := range deniedCIDRs {
		if cidr.Contains(ip) {
			return false
		}
	}
	return true
}

// ValidateTarget checks an egress URL against the policy: http(s) only, and
// IP-literal hosts must satisfy the address policy. Hostname targets are
// additionally checked at dial time to block DNS rebinding.
func ValidateTarget(rawURL string, p Policy) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("egress: invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("egress: scheme %q is not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("egress: url has no host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !p.allowIP(ip) {
			return fmt.Errorf("egress: address %s is not allowed", ip)
		}
	}
	return nil
}

// CheckRedirect enforces the redirect bound and re-validates the target, so a
// redirect cannot escape the address policy.
func (p Policy) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > p.MaxRedirects {
		return errors.New("egress: too many redirects")
	}
	return ValidateTarget(req.URL.String(), p)
}

// DialContext resolves the target and verifies every resolved address against
// the policy before connecting, which blocks DNS rebinding and literal
// internal addresses alike.
func (p Policy) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("egress: unsupported network %q", network)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ipAddrs, err := (&net.Resolver{}).LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	var dialer net.Dialer
	dialer.Timeout = 10 * time.Second
	dialer.KeepAlive = 30 * time.Second
	var lastErr error
	for _, ipAddr := range ipAddrs {
		if !p.allowIP(ipAddr.IP) {
			return nil, fmt.Errorf("egress: address %s for host %q is not allowed", ipAddr.IP, host)
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("egress: host has no resolved addresses")
	}
	return nil, lastErr
}

// Client builds an http.Client enforcing the policy: protected dialing,
// bounded redirects, and capped response bodies. It sets no global timeout so
// long-lived streams can run; callers bound requests with contexts.
func Client(policy Policy) *http.Client {
	proxy := http.ProxyFromEnvironment
	if policy.DisableProxy {
		proxy = nil
	}
	var transport http.RoundTripper = &http.Transport{
		Proxy:                 proxy,
		DialContext:           policy.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	if policy.MaxResponseBytes > 0 {
		transport = &boundedBodyTransport{inner: transport, limit: policy.MaxResponseBytes}
	}
	return &http.Client{Transport: transport, CheckRedirect: policy.CheckRedirect}
}

type boundedBody struct {
	io.Reader
	close func() error
}

func (b *boundedBody) Close() error { return b.close() }

type boundedBodyTransport struct {
	inner http.RoundTripper
	limit int64
}

func (t *boundedBodyTransport) CloseIdleConnections() {
	if closer, ok := t.inner.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func (t *boundedBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.inner.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	original := resp.Body
	resp.Body = &boundedBody{Reader: io.LimitReader(original, t.limit), close: original.Close}
	return resp, nil
}
