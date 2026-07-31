package auth

import (
	"strings"
	"testing"
)

func TestDeviceLabel(t *testing.T) {
	tests := []struct {
		name string
		ua   string
		want string
	}{
		{
			name: "Chrome on Windows",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
			want: "Chrome on Windows",
		},
		{
			name: "Chrome on Android",
			ua:   "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36",
			want: "Chrome on Android",
		},
		{
			name: "Chrome on iOS reports CriOS",
			ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/126.0.0.0 Mobile/15E148 Safari/604.1",
			want: "Chrome on iPhone",
		},
		{
			name: "Safari on iPhone",
			ua:   "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
			want: "Safari on iPhone",
		},
		{
			name: "Safari on iPad",
			ua:   "Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1",
			want: "Safari on iPad",
		},
		{
			name: "Safari on macOS",
			ua:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15",
			want: "Safari on macOS",
		},
		{
			name: "Firefox on Linux",
			ua:   "Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0",
			want: "Firefox on Linux",
		},
		{
			name: "Edge on Windows wins over the Chrome token",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0",
			want: "Edge on Windows",
		},
		{
			name: "Opera wins over the Chrome token",
			ua:   "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 OPR/112.0.0.0",
			want: "Opera on Windows",
		},
		{
			name: "Samsung Internet on Android",
			ua:   "Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/25.0 Chrome/121.0.0.0 Mobile Safari/537.36",
			want: "Samsung Internet on Android",
		},
		{
			name: "ChromeOS",
			ua:   "Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
			want: "Chrome on ChromeOS",
		},
		{
			name: "curl has no platform",
			ua:   "curl/8.5.0",
			want: "curl",
		},
		{
			name: "crawler",
			ua:   "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			want: "Bot",
		},
		{
			name: "platform without a known browser",
			ua:   "SomeApp/1.0 (Windows NT 10.0)",
			want: "Windows",
		},
		{
			name: "empty",
			ua:   "",
			want: UnknownDeviceLabel,
		},
		{
			name: "whitespace only",
			ua:   "   ",
			want: UnknownDeviceLabel,
		},
		{
			name: "garbage",
			ua:   "???",
			want: UnknownDeviceLabel,
		},
		{
			name: "long garbage",
			ua:   strings.Repeat("x", 2000),
			want: UnknownDeviceLabel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeviceLabel(tt.ua); got != tt.want {
				t.Errorf("DeviceLabel(%q) = %q, want %q", tt.ua, got, tt.want)
			}
		})
	}
}
