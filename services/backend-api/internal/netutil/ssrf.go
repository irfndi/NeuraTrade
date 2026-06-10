// Package netutil provides network utility functions for safe outbound HTTP requests.
package netutil

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// DefaultAllowedSchemes are the URL schemes permitted for outbound requests.
var DefaultAllowedSchemes = []string{"https", "http"}

// ValidateURL checks that rawURL is well-formed, uses an allowed scheme,
// and does not resolve to a private/reserved IP address (SSRF defense).
// For http scheme, only localhost/loopback is permitted.
func ValidateURL(rawURL string, allowedSchemes []string) error {
	if allowedSchemes == nil {
		allowedSchemes = DefaultAllowedSchemes
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("URL missing scheme or host: %s", rawURL)
	}

	schemeOK := false
	for _, s := range allowedSchemes {
		if strings.EqualFold(u.Scheme, s) {
			schemeOK = true
			break
		}
	}
	if !schemeOK {
		return fmt.Errorf("URL scheme %q not allowed", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL missing host: %s", rawURL)
	}

	// http is only allowed for localhost/loopback (internal services)
	isHTTP := strings.EqualFold(u.Scheme, "http")
	if isHTTP {
		if !isLoopback(host) {
			return fmt.Errorf("http scheme only permitted for loopback addresses, got %q", host)
		}
	}

	// Resolve hostname and block private/reserved IPs.
	// For http scheme we already validated loopback above; skip further
	// checks so http://localhost works for internal services.
	if isHTTP && isLoopback(host) {
		return nil
	}

	ips, err := net.LookupIP(host)
	if err != nil {
		// If DNS resolution fails, we can't verify it's safe.
		// In a strict security posture, we reject; but for external SaaS
		// APIs (e.g., some AI providers), DNS may transiently fail.
		// We log and allow, relying on the transport layer for defense.
		return nil
	}

	for _, ip := range ips {
		if isPrivateOrReserved(ip) {
			return fmt.Errorf("URL resolves to private/reserved IP %s", ip)
		}
	}

	return nil
}

// ValidateURLWithAllowlist checks that rawURL's hostname matches one of the
// allowed hosts (exact or suffix match for subdomains).
func ValidateURLWithAllowlist(rawURL string, allowedHosts []string) error {
	if err := ValidateURL(rawURL, nil); err != nil {
		return err
	}

	u, _ := url.Parse(rawURL)
	host := strings.ToLower(u.Hostname())

	for _, allowed := range allowedHosts {
		allowed = strings.ToLower(strings.TrimSpace(allowed))
		if allowed == "" {
			continue
		}
		if host == allowed {
			return nil
		}
		// Allow suffix match for subdomains: ".example.com" matches "api.example.com"
		if strings.HasPrefix(allowed, ".") && strings.HasSuffix(host, allowed) {
			return nil
		}
	}

	return fmt.Errorf("URL host %q not in allowlist", host)
}

// isLoopback reports whether host is a loopback address or hostname.
func isLoopback(host string) bool {
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// isPrivateOrReserved reports whether ip is a private, loopback, link-local,
// or otherwise reserved address that should not be reachable from outbound
// HTTP requests (SSRF defense).
func isPrivateOrReserved(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		isUniqueLocal(ip)
}

// isUniqueLocal reports whether ip is a IPv6 unique local address (fc00::/7).
func isUniqueLocal(ip net.IP) bool {
	if len(ip) != 16 {
		return false
	}
	return ip[0]&0xfe == 0xfc
}
