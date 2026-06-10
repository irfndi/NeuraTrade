package netutil

import (
	"net"
	"testing"
)

func TestValidateURL_Allowed(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"https public", "https://api.example.com/v1"},
		{"https with port", "https://api.example.com:443/v1"},
		{"http localhost", "http://localhost:8080/health"},
		{"http 127.0.0.1", "http://127.0.0.1:3001/api"},
		{"http ::1", "http://[::1]:8080/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateURL(tc.url, nil); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestValidateURL_Blocked(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"private 10.x", "https://10.0.0.1/api"},
		{"private 192.168.x", "https://192.168.1.1/api"},
		{"private 172.16.x", "https://172.16.0.1/api"},
		{"private 172.31.x", "https://172.31.255.1/api"},
		{"link local 169.254", "https://169.254.1.1/api"},
		{"loopback 127.0.0.1 https", "https://127.0.0.1/api"},
		{"localhost https", "https://localhost/api"},
		{"http non-loopback", "http://api.example.com/health"},
		{"missing scheme", "api.example.com/v1"},
		{"file scheme", "file:///etc/passwd"},
		{"ftp scheme", "ftp://ftp.example.com/file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateURL(tc.url, nil); err == nil {
				t.Fatalf("expected error for %q, got nil", tc.url)
			}
		})
	}
}

func TestValidateURLWithAllowlist(t *testing.T) {
	cases := []struct {
		name         string
		url          string
		allowlist    []string
		expectErr    bool
	}{
		{"exact match", "https://api.openai.com/v1", []string{"api.openai.com"}, false},
		{"suffix match", "https://api.openai.com/v1", []string{".openai.com"}, false},
		{"no match", "https://evil.com/api", []string{"api.openai.com"}, true},
		{"wrong suffix", "https://notopenai.com/api", []string{".openai.com"}, true},
		{"case insensitive", "https://API.OPENAI.COM/v1", []string{"api.openai.com"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateURLWithAllowlist(tc.url, tc.allowlist)
			if tc.expectErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tc.url)
			}
			if !tc.expectErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}

func TestIsPrivateOrReserved(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"8.8.8.8", false},           // public
		{"1.1.1.1", false},           // public
		{"127.0.0.1", true},          // loopback
		{"10.0.0.1", true},           // private
		{"192.168.1.1", true},        // private
		{"172.16.0.1", true},         // private
		{"172.31.255.1", true},       // private
		{"169.254.1.1", true},        // link-local
		{"224.0.0.1", true},          // multicast
		{"::1", true},                // loopback IPv6
		{"fe80::1", true},            // link-local IPv6
		{"fc00::1", true},            // unique local IPv6
		{"2001:4860:4860::8888", false}, // public IPv6
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			parsed := net.ParseIP(tc.ip)
			if parsed == nil {
				t.Fatalf("failed to parse IP %q", tc.ip)
			}
			got := isPrivateOrReserved(parsed)
			if got != tc.blocked {
				t.Fatalf("isPrivateOrReserved(%q) = %v, want %v", tc.ip, got, tc.blocked)
			}
		})
	}
}

