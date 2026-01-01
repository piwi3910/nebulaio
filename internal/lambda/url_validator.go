package lambda

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// validateWebhookURL validates a webhook URL to prevent SSRF attacks.
// It blocks requests to:
// - localhost and loopback addresses (127.0.0.0/8, ::1).
// - private IP ranges (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16).
// - link-local addresses (169.254.0.0/16, fe80::/10).
// - IPv6 unique local addresses (fc00::/7).
func validateWebhookURL(ctx context.Context, urlStr string) error {
	// Parse URL
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Only allow HTTP and HTTPS
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid URL scheme: %s (only http and https allowed)", u.Scheme)
	}

	// Extract hostname
	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("URL must have a hostname")
	}

	// Block localhost and common aliases
	if isLocalhost(hostname) {
		return fmt.Errorf("webhook URL cannot target localhost")
	}

	// Resolve hostname to IP addresses with context timeout
	resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resolver := &net.Resolver{}

	ipAddrs, err := resolver.LookupIPAddr(resolveCtx, hostname)
	if err != nil {
		return fmt.Errorf("failed to resolve hostname: %w", err)
	}

	// Check all resolved IPs
	for _, ipAddr := range ipAddrs {
		if err := validateIP(ipAddr.IP); err != nil {
			return fmt.Errorf("webhook URL resolves to blocked IP %s: %w", ipAddr.IP.String(), err)
		}
	}

	return nil
}

// isLocalhost checks if hostname is localhost or a common alias.
func isLocalhost(hostname string) bool {
	hostname = strings.ToLower(hostname)
	localhostAliases := []string{
		"localhost",
		"localhost.localdomain",
		"127.0.0.1",
		"::1",
		"0.0.0.0",
		"::",
	}

	for _, alias := range localhostAliases {
		if hostname == alias {
			return true
		}
	}

	return false
}

// validateIP checks if an IP address is blocked for SSRF protection.
func validateIP(ip net.IP) error {
	// Check if it's a valid IP
	if ip == nil {
		return fmt.Errorf("invalid IP address")
	}

	// Check for loopback
	if ip.IsLoopback() {
		return fmt.Errorf("loopback addresses are blocked")
	}

	// Check for link-local
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local addresses are blocked")
	}

	// Check for private networks
	if isPrivateIP(ip) {
		return fmt.Errorf("private IP addresses are blocked")
	}

	// Check for multicast
	if ip.IsMulticast() {
		return fmt.Errorf("multicast addresses are blocked")
	}

	// Check for unspecified (0.0.0.0, ::)
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified addresses are blocked")
	}

	return nil
}

// isPrivateIP checks if an IP is in a private range.
func isPrivateIP(ip net.IP) bool {
	// Define private IP ranges
	privateRanges := []string{
		"10.0.0.0/8",         // RFC1918
		"172.16.0.0/12",      // RFC1918
		"192.168.0.0/16",     // RFC1918
		"169.254.0.0/16",     // RFC3927 link-local
		"fc00::/7",           // RFC4193 IPv6 unique local
		"fe80::/10",          // RFC4291 IPv6 link-local
		"100.64.0.0/10",      // RFC6598 shared address space
		"192.0.0.0/24",       // RFC6890 IETF protocol assignments
		"192.0.2.0/24",       // RFC5737 TEST-NET-1
		"198.51.100.0/24",    // RFC5737 TEST-NET-2
		"203.0.113.0/24",     // RFC5737 TEST-NET-3
		"192.88.99.0/24",     // RFC3068 6to4 relay anycast
		"198.18.0.0/15",      // RFC2544 benchmarking
		"224.0.0.0/4",        // RFC5771 multicast
		"240.0.0.0/4",        // RFC6890 reserved
		"255.255.255.255/32", // RFC8190 broadcast
	}

	for _, cidr := range privateRanges {
		_, subnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}

		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}
