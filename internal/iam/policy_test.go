package iam

import (
	"testing"
)

// TestIPMatchesIPv6 tests IPv6 CIDR matching functionality
func TestIPMatchesIPv6(t *testing.T) {
	pe := &PolicyEvaluator{}

	tests := []struct {
		name     string
		ip       string
		cidr     string
		expected bool
	}{
		// IPv6 CIDR matching
		{
			name:     "IPv6 address within CIDR range",
			ip:       "2001:db8::1",
			cidr:     "2001:db8::/32",
			expected: true,
		},
		{
			name:     "IPv6 address outside CIDR range",
			ip:       "2001:db9::1",
			cidr:     "2001:db8::/32",
			expected: false,
		},
		{
			name:     "IPv6 address at start of /64 range",
			ip:       "2001:db8:abcd:1234::",
			cidr:     "2001:db8:abcd:1234::/64",
			expected: true,
		},
		{
			name:     "IPv6 address at end of /64 range",
			ip:       "2001:db8:abcd:1234:ffff:ffff:ffff:ffff",
			cidr:     "2001:db8:abcd:1234::/64",
			expected: true,
		},
		// IPv6 exact matching
		{
			name:     "IPv6 exact match - same address",
			ip:       "2001:db8::1",
			cidr:     "2001:db8::1",
			expected: true,
		},
		{
			name:     "IPv6 exact match - different address",
			ip:       "2001:db8::1",
			cidr:     "2001:db8::2",
			expected: false,
		},
		{
			name:     "IPv6 exact match - equivalent addresses",
			ip:       "2001:0db8:0000:0000:0000:0000:0000:0001",
			cidr:     "2001:db8::1",
			expected: true,
		},
		// Mixed IPv4/IPv6 scenarios (cross-protocol validation)
		{
			name:     "IPv4 address vs IPv6 CIDR - should not match",
			ip:       "192.168.1.1",
			cidr:     "2001:db8::/32",
			expected: false,
		},
		{
			name:     "IPv6 address vs IPv4 CIDR - should not match",
			ip:       "2001:db8::1",
			cidr:     "192.168.1.0/24",
			expected: false,
		},
		// IPv6 link-local addresses (fe80::/10)
		{
			name:     "IPv6 link-local address within range",
			ip:       "fe80::1",
			cidr:     "fe80::/10",
			expected: true,
		},
		{
			name:     "IPv6 link-local address - different interface ID",
			ip:       "fe80::abcd:1234:5678:9abc",
			cidr:     "fe80::/10",
			expected: true,
		},
		{
			name:     "IPv6 non-link-local address vs link-local CIDR",
			ip:       "2001:db8::1",
			cidr:     "fe80::/10",
			expected: false,
		},
		// IPv4-mapped IPv6 addresses (::ffff:x.x.x.x)
		{
			name:     "IPv4-mapped IPv6 address - exact match",
			ip:       "::ffff:192.168.1.1",
			cidr:     "::ffff:192.168.1.1",
			expected: true,
		},
		{
			name:     "IPv4-mapped IPv6 CIDR range",
			ip:       "::ffff:192.168.1.100",
			cidr:     "::ffff:192.168.1.0/120", // /120 in IPv6 = /24 in the mapped IPv4
			expected: true,
		},
		// Loopback addresses
		{
			name:     "IPv6 loopback address exact match",
			ip:       "::1",
			cidr:     "::1",
			expected: true,
		},
		{
			name:     "IPv6 loopback vs non-loopback",
			ip:       "::1",
			cidr:     "2001:db8::/32",
			expected: false,
		},
		// Edge cases
		{
			name:     "IPv6 /128 CIDR - single host",
			ip:       "2001:db8::1",
			cidr:     "2001:db8::1/128",
			expected: true,
		},
		{
			name:     "IPv6 /128 CIDR - different host",
			ip:       "2001:db8::2",
			cidr:     "2001:db8::1/128",
			expected: false,
		},
		{
			name:     "IPv6 ::/0 - all addresses",
			ip:       "2001:db8:abcd::1234",
			cidr:     "::/0",
			expected: true,
		},
		// IPv4 control tests (ensure IPv4 still works)
		{
			name:     "IPv4 address within CIDR range",
			ip:       "192.168.1.100",
			cidr:     "192.168.1.0/24",
			expected: true,
		},
		{
			name:     "IPv4 address outside CIDR range",
			ip:       "192.168.2.100",
			cidr:     "192.168.1.0/24",
			expected: false,
		},
		{
			name:     "IPv4 exact match",
			ip:       "10.0.0.1",
			cidr:     "10.0.0.1",
			expected: true,
		},
		// Invalid inputs
		{
			name:     "Invalid IPv6 address",
			ip:       "not-an-ipv6-address",
			cidr:     "2001:db8::/32",
			expected: false,
		},
		{
			name:     "Invalid IPv6 CIDR",
			ip:       "2001:db8::1",
			cidr:     "not-a-valid-cidr",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := pe.ipMatches(tc.ip, tc.cidr)
			if result != tc.expected {
				t.Errorf("ipMatches(%s, %s) = %v, expected %v", tc.ip, tc.cidr, result, tc.expected)
			}
		})
	}
}

// TestIPMatchesIPv6SpecialAddresses tests special IPv6 address handling
func TestIPMatchesIPv6SpecialAddresses(t *testing.T) {
	pe := &PolicyEvaluator{}

	tests := []struct {
		name     string
		ip       string
		cidr     string
		expected bool
	}{
		// Unique local addresses (fc00::/7)
		{
			name:     "ULA address within range",
			ip:       "fd00::1",
			cidr:     "fc00::/7",
			expected: true,
		},
		{
			name:     "Non-ULA address vs ULA CIDR",
			ip:       "2001:db8::1",
			cidr:     "fc00::/7",
			expected: false,
		},
		// Multicast addresses (ff00::/8)
		{
			name:     "Multicast address within range",
			ip:       "ff02::1",
			cidr:     "ff00::/8",
			expected: true,
		},
		{
			name:     "Non-multicast vs multicast CIDR",
			ip:       "2001:db8::1",
			cidr:     "ff00::/8",
			expected: false,
		},
		// Documentation addresses (2001:db8::/32)
		{
			name:     "Documentation prefix",
			ip:       "2001:db8:1234::1",
			cidr:     "2001:db8::/32",
			expected: true,
		},
		// 6to4 addresses (2002::/16)
		{
			name:     "6to4 address within range",
			ip:       "2002:c0a8:0101::1",
			cidr:     "2002::/16",
			expected: true,
		},
		{
			name:     "Non-6to4 address vs 6to4 CIDR",
			ip:       "2001:db8::1",
			cidr:     "2002::/16",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := pe.ipMatches(tc.ip, tc.cidr)
			if result != tc.expected {
				t.Errorf("ipMatches(%s, %s) = %v, expected %v", tc.ip, tc.cidr, result, tc.expected)
			}
		})
	}
}

// TestIPMatchesIPv6Normalization tests that different representations of the same address match
func TestIPMatchesIPv6Normalization(t *testing.T) {
	pe := &PolicyEvaluator{}

	tests := []struct {
		name     string
		ip       string
		cidr     string
		expected bool
	}{
		{
			name:     "Full form vs compressed form",
			ip:       "2001:0db8:0000:0000:0000:0000:0000:0001",
			cidr:     "2001:db8::1",
			expected: true,
		},
		{
			name:     "Mixed case in hex digits",
			ip:       "2001:DB8::1",
			cidr:     "2001:db8::1",
			expected: true,
		},
		{
			name:     "Leading zeros stripped vs present",
			ip:       "2001:db8:0:0:0:0:0:1",
			cidr:     "2001:db8::1",
			expected: true,
		},
		{
			name:     "Compressed vs full in CIDR",
			ip:       "2001:db8::1",
			cidr:     "2001:0db8:0000:0000:0000:0000:0000:0000/32",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := pe.ipMatches(tc.ip, tc.cidr)
			if result != tc.expected {
				t.Errorf("ipMatches(%s, %s) = %v, expected %v", tc.ip, tc.cidr, result, tc.expected)
			}
		})
	}
}
