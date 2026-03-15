package infra

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ValidateServiceURL checks that a URL is safe to make HTTP requests to.
// It rejects private/internal IP ranges, localhost, and non-HTTP(S) schemes
// to prevent SSRF attacks. This is used for early rejection at the handler
// level; the actual SSRF enforcement happens at connection time via
// safeDialContext to prevent DNS rebinding attacks.
func ValidateServiceURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("unsupported scheme %q: only http and https are allowed", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL must contain a hostname")
	}

	if portStr := u.Port(); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("port must be between 1 and 65535")
		}
	}

	if err := validateHost(host); err != nil {
		return err
	}

	return nil
}

// ValidateHostname checks that a hostname is safe to connect to for TLS
// certificate checks. Rejects private IPs, localhost, and bare IPs in
// private ranges. This is used for early rejection at the handler level;
// the actual SSRF enforcement happens at connection time via safeDialContext.
func ValidateHostname(hostname string) error {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return fmt.Errorf("hostname must not be empty")
	}
	return validateHost(hostname)
}

func validateHost(host string) error {
	// Block localhost variants.
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "localhost." {
		return fmt.Errorf("localhost is not allowed")
	}

	// If the host is an IP address, check it directly.
	ip := net.ParseIP(host)
	if ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("private/internal IP addresses are not allowed")
		}
		return nil
	}

	// Resolve the hostname and check all resulting IPs.
	addrs, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("could not resolve hostname %q: %w", host, err)
	}

	for _, addr := range addrs {
		resolved := net.ParseIP(addr)
		if resolved != nil && isPrivateIP(resolved) {
			return fmt.Errorf("hostname %q resolves to a private/internal IP address", host)
		}
	}

	return nil
}

// safeDialContext is a DialContext function that validates resolved IP addresses
// before establishing a connection. This prevents DNS rebinding attacks where a
// hostname resolves to a public IP during validation but to a private IP when
// the actual connection is made.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("invalid address %q: %w", addr, err)
	}

	// Block localhost at dial time too.
	lower := strings.ToLower(host)
	if lower == "localhost" || lower == "localhost." {
		return nil, fmt.Errorf("connection to localhost is blocked")
	}

	// Resolve the hostname ourselves so we can inspect the IPs.
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed for %q: %w", host, err)
	}

	// Check every resolved IP before connecting.
	for _, ipAddr := range ips {
		if isPrivateIP(ipAddr.IP) {
			return nil, fmt.Errorf("connection to private/internal IP %s blocked (resolved from %q)", ipAddr.IP, host)
		}
	}

	// Connect to the first resolved IP to avoid a second resolution.
	var dialer net.Dialer
	for _, ipAddr := range ips {
		target := net.JoinHostPort(ipAddr.IP.String(), port)
		conn, dialErr := dialer.DialContext(ctx, network, target)
		if dialErr != nil {
			err = dialErr
			continue
		}
		return conn, nil
	}

	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("no addresses resolved for %q", host)
}

// isPrivateIP returns true for loopback, private, link-local, and other
// non-routable IP addresses.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}

	// IPv4 metadata address (169.254.169.254) used by cloud providers.
	if ip.Equal(net.ParseIP("169.254.169.254")) {
		return true
	}

	return false
}
