package infra

import (
	"net"
	"strings"
	"testing"
)

func TestValidateServiceURL_ValidURLs(t *testing.T) {
	// These may fail DNS resolution in CI, so we only test the scheme/parse logic.
	tests := []struct {
		name string
		url  string
	}{
		{"https", "https://example.com/health"},
		{"http", "http://example.com:8080/api/status"},
		{"https with path", "https://api.example.com/v1/health"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceURL(tt.url)
			// DNS resolution may fail in test environments, so only check
			// that it doesn't fail for scheme/parse reasons.
			if err != nil && !strings.Contains(err.Error(), "resolve") {
				t.Errorf("ValidateServiceURL(%q) = %v, want nil or DNS error", tt.url, err)
			}
		})
	}
}

func TestValidateServiceURL_InvalidScheme(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"ftp scheme", "ftp://example.com/file", "unsupported scheme"},
		{"file scheme", "file:///etc/passwd", "unsupported scheme"},
		{"gopher scheme", "gopher://evil.com", "unsupported scheme"},
		{"no scheme", "example.com/health", "unsupported scheme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceURL(tt.url)
			if err == nil {
				t.Fatalf("expected error for %q, got nil", tt.url)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("expected error containing %q, got: %v", tt.want, err)
			}
		})
	}
}

func TestValidateServiceURL_Localhost(t *testing.T) {
	tests := []string{
		"http://localhost/health",
		"http://localhost:8080/api",
		"https://LOCALHOST/health",
	}
	for _, url := range tests {
		err := ValidateServiceURL(url)
		if err == nil {
			t.Errorf("expected error for localhost URL %q, got nil", url)
		}
		if !strings.Contains(err.Error(), "localhost") {
			t.Errorf("expected 'localhost' in error for %q, got: %v", url, err)
		}
	}
}

func TestValidateServiceURL_PrivateIPs(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"loopback IPv4", "http://127.0.0.1/health"},
		{"loopback IPv6", "http://[::1]/health"},
		{"private 10.x", "http://10.0.0.1:8080/api"},
		{"private 172.16.x", "http://172.16.0.1/api"},
		{"private 192.168.x", "http://192.168.1.1/api"},
		{"metadata IP", "http://169.254.169.254/latest/meta-data/"},
		{"unspecified", "http://0.0.0.0/health"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceURL(tt.url)
			if err == nil {
				t.Errorf("expected error for private IP URL %q, got nil", tt.url)
			}
		})
	}
}

func TestValidateServiceURL_InvalidPort(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"port too high", "http://example.com:99999/health"},
		{"port zero", "http://example.com:0/health"},
		{"port negative", "http://example.com:-1/health"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateServiceURL(tt.url)
			if err == nil {
				t.Errorf("expected error for invalid port URL %q, got nil", tt.url)
			}
		})
	}
}

func TestValidateServiceURL_EmptyHost(t *testing.T) {
	err := ValidateServiceURL("http:///path")
	if err == nil {
		t.Error("expected error for URL with empty host")
	}
}

func TestValidateHostname_Valid(t *testing.T) {
	err := ValidateHostname("example.com")
	// DNS resolution might fail in test env, that's fine.
	if err != nil && !strings.Contains(err.Error(), "resolve") {
		t.Errorf("ValidateHostname(example.com) = %v, want nil or DNS error", err)
	}
}

func TestValidateHostname_Localhost(t *testing.T) {
	err := ValidateHostname("localhost")
	if err == nil {
		t.Error("expected error for localhost hostname")
	}
	if !strings.Contains(err.Error(), "localhost") {
		t.Errorf("expected 'localhost' in error, got: %v", err)
	}
}

func TestValidateHostname_PrivateIP(t *testing.T) {
	tests := []string{"127.0.0.1", "10.0.0.1", "192.168.1.1", "169.254.169.254", "0.0.0.0"}
	for _, host := range tests {
		err := ValidateHostname(host)
		if err == nil {
			t.Errorf("expected error for private IP hostname %q, got nil", host)
		}
	}
}

func TestValidateHostname_Empty(t *testing.T) {
	err := ValidateHostname("")
	if err == nil {
		t.Error("expected error for empty hostname")
	}
}

func TestValidateHostname_EmbeddedPort(t *testing.T) {
	tests := []string{
		"localhost:8080",
		"localhost:443",
		"example.com:22",
		"127.0.0.1:80",
	}
	for _, host := range tests {
		err := ValidateHostname(host)
		if err == nil {
			t.Errorf("expected error for hostname with embedded port %q, got nil", host)
		}
	}
}

func TestValidateServiceURL_HostnameWithPort(t *testing.T) {
	// Ensure URLs with private IPs and ports are rejected.
	tests := []string{
		"http://127.0.0.1:8080/admin",
		"http://[::1]:80/admin",
		"http://10.0.0.1:3000/internal",
	}
	for _, u := range tests {
		err := ValidateServiceURL(u)
		if err == nil {
			t.Errorf("expected error for URL %q, got nil", u)
		}
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := parseIPForTest(t, tt.ip)
			got := isPrivateIP(ip)
			if got != tt.private {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestSafeDialContext_BlocksPrivateIPs(t *testing.T) {
	// safeDialContext should block connections to private IPs at dial time.
	tests := []struct {
		name string
		addr string
	}{
		{"loopback", "127.0.0.1:80"},
		{"private 10.x", "10.0.0.1:80"},
		{"private 192.168.x", "192.168.1.1:80"},
		{"metadata", "169.254.169.254:80"},
		{"localhost", "localhost:80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := safeDialContext(t.Context(), "tcp", tt.addr)
			if err == nil {
				t.Errorf("expected safeDialContext to block %q, got nil error", tt.addr)
			}
		})
	}
}

func TestValidateDNSHostname_Valid(t *testing.T) {
	tests := []string{
		"example.com",
		"sub.example.com",
		"deep.sub.example.com",
	}
	for _, h := range tests {
		if err := ValidateDNSHostname(h); err != nil {
			t.Errorf("ValidateDNSHostname(%q) = %v, want nil", h, err)
		}
	}
}

func TestValidateDNSHostname_RejectsIPs(t *testing.T) {
	tests := []string{"127.0.0.1", "10.0.0.1", "8.8.8.8", "::1"}
	for _, h := range tests {
		if err := ValidateDNSHostname(h); err == nil {
			t.Errorf("ValidateDNSHostname(%q) = nil, want error for IP address", h)
		}
	}
}

func TestValidateDNSHostname_RejectsLocalhost(t *testing.T) {
	if err := ValidateDNSHostname("localhost"); err == nil {
		t.Error("expected error for localhost")
	}
}

func TestValidateDNSHostname_RejectsInternalDomains(t *testing.T) {
	tests := []string{
		"db-server.local",
		"app.internal",
		"mail.corp",
		"nas.lan",
		"router.home",
		"host.localdomain",
		"wiki.intranet",
	}
	for _, h := range tests {
		if err := ValidateDNSHostname(h); err == nil {
			t.Errorf("ValidateDNSHostname(%q) = nil, want error for internal domain", h)
		}
	}
}

func TestValidateDNSHostname_RejectsSingleLabel(t *testing.T) {
	tests := []string{"db-server", "myhost", "intranet"}
	for _, h := range tests {
		if err := ValidateDNSHostname(h); err == nil {
			t.Errorf("ValidateDNSHostname(%q) = nil, want error for single-label hostname", h)
		}
	}
}

func TestValidateDNSHostname_Empty(t *testing.T) {
	if err := ValidateDNSHostname(""); err == nil {
		t.Error("expected error for empty hostname")
	}
}

func TestFilterPrivateIPs(t *testing.T) {
	input := []string{"8.8.8.8", "192.168.1.1", "1.1.1.1", "10.0.0.1", "127.0.0.1"}
	got := FilterPrivateIPs(input)
	want := []string{"8.8.8.8", "1.1.1.1"}
	if len(got) != len(want) {
		t.Fatalf("FilterPrivateIPs: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("FilterPrivateIPs[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilterPrivateIPs_NonIPValues(t *testing.T) {
	// Non-IP strings (like MX hostnames) should pass through.
	input := []string{"mail.example.com.", "8.8.8.8"}
	got := FilterPrivateIPs(input)
	if len(got) != 2 {
		t.Errorf("expected non-IP values to pass through, got %v", got)
	}
}

func parseIPForTest(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("failed to parse IP: %s", s)
	}
	return ip
}
