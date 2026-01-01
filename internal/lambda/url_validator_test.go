package lambda

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		shouldErr bool
		errMsg    string
	}{
		// Valid URLs
		{
			name:      "valid public HTTPS URL",
			url:       "https://api.example.com/webhook",
			shouldErr: false,
		},
		{
			name:      "valid public HTTP URL",
			url:       "http://webhook.example.com/endpoint",
			shouldErr: false,
		},

		// Invalid schemes
		{
			name:      "file scheme blocked",
			url:       "file:///etc/passwd",
			shouldErr: true,
			errMsg:    "invalid URL scheme",
		},
		{
			name:      "ftp scheme blocked",
			url:       "ftp://example.com/file",
			shouldErr: true,
			errMsg:    "invalid URL scheme",
		},

		// Localhost variations
		{
			name:      "localhost hostname blocked",
			url:       "http://localhost/admin",
			shouldErr: true,
			errMsg:    "localhost",
		},
		{
			name:      "127.0.0.1 blocked",
			url:       "http://127.0.0.1:9001/admin",
			shouldErr: true,
			errMsg:    "localhost",
		},
		{
			name:      "127.x.x.x loopback blocked",
			url:       "http://127.0.0.2/admin",
			shouldErr: true,
			errMsg:    "loopback",
		},
		{
			name:      "IPv6 loopback blocked",
			url:       "http://[::1]/admin",
			shouldErr: true,
			errMsg:    "localhost",
		},

		// Private IP ranges
		{
			name:      "10.0.0.0/8 blocked",
			url:       "http://10.0.0.1/internal",
			shouldErr: true,
			errMsg:    "private IP",
		},
		{
			name:      "172.16.0.0/12 blocked",
			url:       "http://172.16.0.1/internal",
			shouldErr: true,
			errMsg:    "private IP",
		},
		{
			name:      "192.168.0.0/16 blocked",
			url:       "http://192.168.1.1/router",
			shouldErr: true,
			errMsg:    "private IP",
		},

		// Link-local
		{
			name:      "169.254.0.0/16 link-local blocked",
			url:       "http://169.254.169.254/metadata",
			shouldErr: true,
			errMsg:    "private IP",
		},

		// Invalid URLs
		{
			name:      "malformed URL",
			url:       "not a url",
			shouldErr: true,
			errMsg:    "invalid URL",
		},
		{
			name:      "URL without hostname",
			url:       "http://",
			shouldErr: true,
			errMsg:    "hostname",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebhookURL(t.Context(), tt.url)

			if tt.shouldErr {
				require.Error(t, err, "Expected error for URL: %s", tt.url)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg,
						"Error message should contain '%s' for URL: %s", tt.errMsg, tt.url)
				}
			} else {
				assert.NoError(t, err, "Expected no error for URL: %s", tt.url)
			}
		})
	}
}

func TestIsLocalhost(t *testing.T) {
	tests := []struct {
		hostname string
		expected bool
	}{
		{"localhost", true},
		{"localhost.localdomain", true},
		{"LOCALHOST", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"0.0.0.0", true},
		{"::", true},
		{"example.com", false},
		{"local", false},
		{"192.168.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.hostname, func(t *testing.T) {
			result := isLocalhost(tt.hostname)
			assert.Equal(t, tt.expected, result,
				"isLocalhost(%s) should be %v", tt.hostname, tt.expected)
		})
	}
}

func TestValidateIP(t *testing.T) {
	tests := []struct {
		name      string
		ip        string
		shouldErr bool
		errMsg    string
	}{
		// Valid public IPs
		{
			name:      "public IPv4",
			ip:        "8.8.8.8",
			shouldErr: false,
		},
		{
			name:      "public IPv4 2",
			ip:        "1.1.1.1",
			shouldErr: false,
		},

		// Loopback
		{
			name:      "IPv4 loopback",
			ip:        "127.0.0.1",
			shouldErr: true,
			errMsg:    "loopback",
		},
		{
			name:      "IPv6 loopback",
			ip:        "::1",
			shouldErr: true,
			errMsg:    "loopback",
		},

		// Private ranges
		{
			name:      "10.0.0.0/8",
			ip:        "10.1.2.3",
			shouldErr: true,
			errMsg:    "private IP",
		},
		{
			name:      "172.16.0.0/12",
			ip:        "172.20.0.1",
			shouldErr: true,
			errMsg:    "private IP",
		},
		{
			name:      "192.168.0.0/16",
			ip:        "192.168.100.1",
			shouldErr: true,
			errMsg:    "private IP",
		},

		// Link-local
		{
			name:      "IPv4 link-local",
			ip:        "169.254.1.1",
			shouldErr: true,
			errMsg:    "private IP",
		},
		{
			name:      "IPv6 link-local",
			ip:        "fe80::1",
			shouldErr: true,
			errMsg:    "link-local",
		},

		// Multicast
		{
			name:      "IPv4 multicast",
			ip:        "224.0.0.1",
			shouldErr: true,
			errMsg:    "private IP",
		},

		// Unspecified
		{
			name:      "IPv4 unspecified",
			ip:        "0.0.0.0",
			shouldErr: true,
			errMsg:    "unspecified",
		},
		{
			name:      "IPv6 unspecified",
			ip:        "::",
			shouldErr: true,
			errMsg:    "unspecified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := parseIP(tt.ip)
			err := validateIP(ip)

			if tt.shouldErr {
				require.Error(t, err, "Expected error for IP: %s", tt.ip)

				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg,
						"Error message should contain '%s' for IP: %s", tt.errMsg, tt.ip)
				}
			} else {
				assert.NoError(t, err, "Expected no error for IP: %s", tt.ip)
			}
		})
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip       string
		expected bool
	}{
		// Public IPs
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2001:4860:4860::8888", false},

		// Private ranges
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"192.168.255.255", true},

		// Link-local
		{"169.254.0.1", true},
		{"169.254.255.255", true},
		{"fe80::1", true},

		// IPv6 unique local
		{"fc00::1", true},
		{"fd00::1", true},

		// Shared address space
		{"100.64.0.1", true},

		// Test networks
		{"192.0.2.1", true},
		{"198.51.100.1", true},
		{"203.0.113.1", true},

		// Multicast
		{"224.0.0.1", true},

		// Broadcast
		{"255.255.255.255", true},
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := parseIP(tt.ip)
			result := isPrivateIP(ip)
			assert.Equal(t, tt.expected, result,
				"isPrivateIP(%s) should be %v", tt.ip, tt.expected)
		})
	}
}

// parseIP is a helper to parse IP addresses for testing.
func parseIP(ipStr string) net.IP {
	return net.ParseIP(ipStr)
}
