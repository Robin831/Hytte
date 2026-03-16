package infra

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"regexp"
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

// internalDomainSuffixes are TLD/suffixes commonly used for internal networks.
var internalDomainSuffixes = []string{
	".local",
	".internal",
	".corp",
	".lan",
	".home",
	".localdomain",
	".intranet",
}

// ValidateDNSHostname validates a hostname for DNS monitoring. It rejects
// IP addresses (DNS monitors should use domain names), localhost, internal
// domain suffixes, and single-label hostnames that likely refer to internal
// hosts. Unlike ValidateHostname, it does NOT resolve the hostname — that
// would defeat the purpose since DNS resolution is the monitoring action.
func ValidateDNSHostname(hostname string) error {
	// Reject hostnames containing any whitespace before trimming, so callers
	// cannot sneak leading/trailing spaces past the empty check.
	if strings.ContainsAny(hostname, " \t\r\n") {
		return fmt.Errorf("hostname must not contain whitespace")
	}

	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return fmt.Errorf("hostname must not be empty")
	}

	// Reject port-like syntax (e.g. "example.com:53").
	if strings.Contains(hostname, ":") {
		return fmt.Errorf("hostname must not contain a port; provide the hostname only")
	}

	// Reject consecutive dots (e.g. "example..com").
	if strings.Contains(hostname, "..") {
		return fmt.Errorf("hostname must not contain consecutive dots")
	}

	// Reject bare IP addresses — DNS monitors should use domain names.
	if net.ParseIP(hostname) != nil {
		return fmt.Errorf("IP addresses are not allowed; use a domain name")
	}

	lower := strings.ToLower(hostname)

	// Block localhost.
	if lower == "localhost" || lower == "localhost." {
		return fmt.Errorf("localhost is not allowed")
	}

	// Block internal domain suffixes.
	for _, suffix := range internalDomainSuffixes {
		if strings.HasSuffix(lower, suffix) || strings.HasSuffix(lower, suffix+".") {
			return fmt.Errorf("internal domain suffix %q is not allowed", suffix)
		}
	}

	// Require at least two labels (reject single-label names like "db-server").
	labels := strings.Split(strings.TrimSuffix(hostname, "."), ".")
	if len(labels) < 2 {
		return fmt.Errorf("single-label hostnames are not allowed; use a fully qualified domain name")
	}

	// Validate each label: alphanumeric and hyphens only, no leading/trailing
	// hyphens, non-empty. Trailing dot (FQDN form) is allowed.
	bare := strings.TrimSuffix(hostname, ".")
	for _, label := range strings.Split(bare, ".") {
		if len(label) == 0 {
			return fmt.Errorf("invalid hostname syntax: empty label")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("invalid hostname syntax: label must not start or end with a hyphen")
		}
		if !dnsLabelPattern.MatchString(label) {
			return fmt.Errorf("invalid hostname syntax: label %q contains invalid characters", label)
		}
	}

	return nil
}

// FilterPrivateIPs removes private/internal IP addresses from a slice of
// resolved values. This prevents DNS monitoring results from leaking
// internal network topology.
func FilterPrivateIPs(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, v := range values {
		ip := net.ParseIP(v)
		if ip != nil && isPrivateIP(ip) {
			continue
		}
		filtered = append(filtered, v)
	}
	return filtered
}

// dnsLabelPattern matches a valid DNS label: alphanumeric characters and
// hyphens. Leading/trailing hyphens are checked separately.
var dnsLabelPattern = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// gitHubNamePattern matches valid GitHub owner and repository names:
// alphanumeric characters, hyphens, dots, and underscores, 1-100 chars.
var gitHubNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,99}$`)

// ValidateGitHubOwnerRepo validates that a GitHub owner or repo name contains
// only characters allowed by GitHub. This prevents path injection when the
// values are interpolated into API URLs.
func ValidateGitHubOwnerRepo(owner, repo string) error {
	if !gitHubNamePattern.MatchString(owner) {
		return fmt.Errorf("invalid owner: must contain only alphanumeric characters, hyphens, dots, or underscores")
	}
	if !gitHubNamePattern.MatchString(repo) {
		return fmt.Errorf("invalid repo: must contain only alphanumeric characters, hyphens, dots, or underscores")
	}
	return nil
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

	// Reject hostnames with embedded ports (e.g. "localhost:8080").
	// IPv6 addresses contain colons but are handled above by net.ParseIP.
	if strings.Contains(host, ":") {
		return fmt.Errorf("hostname must not contain a port")
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
