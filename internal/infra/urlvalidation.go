package infra

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateServiceURL checks that a URL is safe to make HTTP requests to.
// It rejects private/internal IP ranges, localhost, and non-HTTP(S) schemes
// to prevent SSRF attacks.
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

	if err := validateHost(host); err != nil {
		return err
	}

	return nil
}

// ValidateHostname checks that a hostname is safe to connect to for TLS
// certificate checks. Rejects private IPs, localhost, and bare IPs in
// private ranges.
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
