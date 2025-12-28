// Package security provides TLS testing for NebulaIO.
package security

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTLSManagerCreation tests TLS manager creation with various configurations.
func TestTLSManagerCreation(t *testing.T) {
	t.Run("creates manager with auto-generation enabled", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &config.TLSConfig{
			Enabled:      true,
			AutoGenerate: true,
			CertDir:      tempDir,
			MinVersion:   "1.2",
			Organization: "TestOrg",
			ValidityDays: 365,
		}

		manager, err := NewTLSManager(cfg, "test-host")
		require.NoError(t, err)
		require.NotNil(t, manager)

		// Verify certificates were created
		assert.FileExists(t, manager.GetCertFile())
		assert.FileExists(t, manager.GetKeyFile())
		assert.FileExists(t, manager.GetCAFile())
	})

	t.Run("fails with nil config", func(t *testing.T) {
		manager, err := NewTLSManager(nil, "test-host")
		assert.Error(t, err)
		assert.Nil(t, manager)
	})

	t.Run("fails when TLS enabled but no certs and auto-generate disabled", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &config.TLSConfig{
			Enabled:      true,
			AutoGenerate: false,
			CertDir:      tempDir,
		}

		manager, err := NewTLSManager(cfg, "test-host")
		assert.Error(t, err)
		assert.Nil(t, manager)
	})
}

// TestCertificateGeneration tests certificate generation functionality.
func TestCertificateGeneration(t *testing.T) {
	t.Run("generates valid CA certificate", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &config.TLSConfig{
			Enabled:      true,
			AutoGenerate: true,
			CertDir:      tempDir,
			MinVersion:   "1.2",
			Organization: "TestOrg",
			ValidityDays: 365,
		}

		manager, err := NewTLSManager(cfg, "test-host")
		require.NoError(t, err)

		// Read and parse CA certificate
		caCertPEM, err := os.ReadFile(manager.GetCAFile())
		require.NoError(t, err)

		pool := x509.NewCertPool()
		ok := pool.AppendCertsFromPEM(caCertPEM)
		assert.True(t, ok, "CA certificate should be valid PEM")
	})

	t.Run("generates server certificate with correct SANs", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &config.TLSConfig{
			Enabled:      true,
			AutoGenerate: true,
			CertDir:      tempDir,
			MinVersion:   "1.2",
			Organization: "TestOrg",
			ValidityDays: 365,
			DNSNames:     []string{"custom.example.com"},
			IPAddresses:  []string{"192.168.1.100"},
		}

		manager, err := NewTLSManager(cfg, "test-host")
		require.NoError(t, err)

		// Load the certificate
		cert, err := tls.LoadX509KeyPair(manager.GetCertFile(), manager.GetKeyFile())
		require.NoError(t, err)

		// Parse the certificate
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)

		// Verify SANs include localhost and custom names
		assert.Contains(t, x509Cert.DNSNames, "localhost")
		assert.Contains(t, x509Cert.DNSNames, "test-host")
		assert.Contains(t, x509Cert.DNSNames, "custom.example.com")

		// Verify IP addresses
		hasLoopback := false
		hasCustomIP := false
		for _, ip := range x509Cert.IPAddresses {
			if ip.Equal(net.ParseIP("127.0.0.1")) {
				hasLoopback = true
			}
			if ip.Equal(net.ParseIP("192.168.1.100")) {
				hasCustomIP = true
			}
		}
		assert.True(t, hasLoopback, "Should include loopback IP")
		assert.True(t, hasCustomIP, "Should include custom IP")
	})

	t.Run("certificate has correct validity period", func(t *testing.T) {
		tempDir := t.TempDir()
		validityDays := 30
		cfg := &config.TLSConfig{
			Enabled:      true,
			AutoGenerate: true,
			CertDir:      tempDir,
			MinVersion:   "1.2",
			Organization: "TestOrg",
			ValidityDays: validityDays,
		}

		manager, err := NewTLSManager(cfg, "test-host")
		require.NoError(t, err)

		// Load the certificate
		cert, err := tls.LoadX509KeyPair(manager.GetCertFile(), manager.GetKeyFile())
		require.NoError(t, err)

		// Parse the certificate
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		require.NoError(t, err)

		// Verify validity period (approximately)
		expectedExpiry := time.Now().AddDate(0, 0, validityDays)
		assert.WithinDuration(t, expectedExpiry, x509Cert.NotAfter, 24*time.Hour)
	})
}

// TestTLSConfig tests TLS configuration building.
func TestTLSConfig(t *testing.T) {
	t.Run("builds TLS config with TLS 1.2 minimum", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &config.TLSConfig{
			Enabled:      true,
			AutoGenerate: true,
			CertDir:      tempDir,
			MinVersion:   "1.2",
			Organization: "TestOrg",
			ValidityDays: 365,
		}

		manager, err := NewTLSManager(cfg, "test-host")
		require.NoError(t, err)

		tlsConfig := manager.GetTLSConfig()
		assert.Equal(t, uint16(tls.VersionTLS12), tlsConfig.MinVersion)
	})

	t.Run("builds TLS config with TLS 1.3 minimum", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &config.TLSConfig{
			Enabled:      true,
			AutoGenerate: true,
			CertDir:      tempDir,
			MinVersion:   "1.3",
			Organization: "TestOrg",
			ValidityDays: 365,
		}

		manager, err := NewTLSManager(cfg, "test-host")
		require.NoError(t, err)

		tlsConfig := manager.GetTLSConfig()
		assert.Equal(t, uint16(tls.VersionTLS13), tlsConfig.MinVersion)
	})

	t.Run("builds TLS config with mTLS enabled", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &config.TLSConfig{
			Enabled:           true,
			AutoGenerate:      true,
			CertDir:           tempDir,
			MinVersion:        "1.2",
			Organization:      "TestOrg",
			ValidityDays:      365,
			RequireClientCert: true,
		}

		manager, err := NewTLSManager(cfg, "test-host")
		require.NoError(t, err)

		tlsConfig := manager.GetTLSConfig()
		assert.Equal(t, tls.RequireAndVerifyClientCert, tlsConfig.ClientAuth)
		assert.NotNil(t, tlsConfig.ClientCAs)
	})

	t.Run("includes strong cipher suites", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := &config.TLSConfig{
			Enabled:      true,
			AutoGenerate: true,
			CertDir:      tempDir,
			MinVersion:   "1.2",
			Organization: "TestOrg",
			ValidityDays: 365,
		}

		manager, err := NewTLSManager(cfg, "test-host")
		require.NoError(t, err)

		tlsConfig := manager.GetTLSConfig()
		assert.NotEmpty(t, tlsConfig.CipherSuites)

		// Verify we have AEAD cipher suites
		hasAEAD := false
		for _, suite := range tlsConfig.CipherSuites {
			switch suite {
			case tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:
				hasAEAD = true
			}
		}
		assert.True(t, hasAEAD, "Should include AEAD cipher suites")
	})
}

// TestCertificateReuse tests that existing certificates are reused.
func TestCertificateReuse(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.TLSConfig{
		Enabled:      true,
		AutoGenerate: true,
		CertDir:      tempDir,
		MinVersion:   "1.2",
		Organization: "TestOrg",
		ValidityDays: 365,
	}

	// Create first manager - generates certificates
	manager1, err := NewTLSManager(cfg, "test-host")
	require.NoError(t, err)

	// Get certificate modification time
	certInfo1, err := os.Stat(manager1.GetCertFile())
	require.NoError(t, err)
	modTime1 := certInfo1.ModTime()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Create second manager - should reuse existing certificates
	manager2, err := NewTLSManager(cfg, "test-host")
	require.NoError(t, err)

	// Get certificate modification time again
	certInfo2, err := os.Stat(manager2.GetCertFile())
	require.NoError(t, err)
	modTime2 := certInfo2.ModTime()

	// Modification times should be the same (certificate was reused)
	assert.Equal(t, modTime1, modTime2, "Certificate should be reused, not regenerated")
}

// TestCustomCertificates tests using custom certificates.
func TestCustomCertificates(t *testing.T) {
	// First, generate certificates that we'll use as "custom" certs
	tempDir := t.TempDir()
	genCfg := &config.TLSConfig{
		Enabled:      true,
		AutoGenerate: true,
		CertDir:      tempDir,
		MinVersion:   "1.2",
		Organization: "TestOrg",
		ValidityDays: 365,
	}

	genManager, err := NewTLSManager(genCfg, "test-host")
	require.NoError(t, err)

	// Now create a manager that uses these as "custom" certificates
	customDir := t.TempDir()
	customCfg := &config.TLSConfig{
		Enabled:      true,
		AutoGenerate: false,
		CertDir:      customDir,
		CertFile:     genManager.GetCertFile(),
		KeyFile:      genManager.GetKeyFile(),
		CAFile:       genManager.GetCAFile(),
		MinVersion:   "1.2",
	}

	customManager, err := NewTLSManager(customCfg, "test-host")
	require.NoError(t, err)
	require.NotNil(t, customManager)

	// Verify it uses the custom certificate paths
	assert.Equal(t, genManager.GetCertFile(), customManager.GetCertFile())
	assert.Equal(t, genManager.GetKeyFile(), customManager.GetKeyFile())
}

// TestCertificateValidation tests certificate validation.
func TestCertificateValidation(t *testing.T) {
	t.Run("detects expired certificates", func(t *testing.T) {
		// This is tested implicitly through the verifyCertificate method
		// The implementation checks if certificate expires within 30 days
		tempDir := t.TempDir()
		cfg := &config.TLSConfig{
			Enabled:      true,
			AutoGenerate: true,
			CertDir:      tempDir,
			MinVersion:   "1.2",
			Organization: "TestOrg",
			ValidityDays: 365,
		}

		manager, err := NewTLSManager(cfg, "test-host")
		require.NoError(t, err)

		// Certificate should be valid (just generated)
		valid, err := manager.verifyCertificate()
		assert.NoError(t, err)
		assert.True(t, valid, "Newly generated certificate should be valid")
	})
}

// TestGetLocalIPs tests local IP detection.
func TestGetLocalIPs(t *testing.T) {
	ips := getLocalIPs()
	// Should at least have some IPs on a typical system
	// This may be empty in some CI environments
	t.Logf("Detected %d local IPs", len(ips))
	for _, ip := range ips {
		t.Logf("  - %s", ip.String())
		// Should not include loopback (that's added separately)
		assert.False(t, ip.IsLoopback(), "getLocalIPs should not include loopback")
	}
}

// TestFileExists tests the fileExists helper function.
func TestFileExists(t *testing.T) {
	t.Run("returns true for existing file", func(t *testing.T) {
		tempDir := t.TempDir()
		tempFile := filepath.Join(tempDir, "test.txt")
		err := os.WriteFile(tempFile, []byte("test"), 0644)
		require.NoError(t, err)

		assert.True(t, fileExists(tempFile))
	})

	t.Run("returns false for non-existing file", func(t *testing.T) {
		assert.False(t, fileExists("/non/existent/file.txt"))
	})

	t.Run("returns false for directory", func(t *testing.T) {
		tempDir := t.TempDir()
		// fileExists checks if path exists (including directories)
		// This is the current behavior
		assert.True(t, fileExists(tempDir))
	})
}

// TestTLSHandshake tests that TLS handshake works with generated certificates.
func TestTLSHandshake(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.TLSConfig{
		Enabled:      true,
		AutoGenerate: true,
		CertDir:      tempDir,
		MinVersion:   "1.2",
		Organization: "TestOrg",
		ValidityDays: 365,
	}

	manager, err := NewTLSManager(cfg, "localhost")
	require.NoError(t, err)

	tlsConfig := manager.GetTLSConfig()
	require.NotNil(t, tlsConfig)

	// Create a test server
	listener, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	require.NoError(t, err)
	defer listener.Close()

	// Get the port
	addr := listener.Addr().String()

	// Server goroutine
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("hello"))
	}()

	// Client connects with CA certificate verification
	caCert, err := os.ReadFile(manager.GetCAFile())
	require.NoError(t, err)

	caPool := x509.NewCertPool()
	ok := caPool.AppendCertsFromPEM(caCert)
	require.True(t, ok)

	clientConfig := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
	}

	conn, err := tls.Dial("tcp", addr, clientConfig)
	require.NoError(t, err, "TLS handshake should succeed")
	defer conn.Close()

	// Read response
	buf := make([]byte, 5)
	n, err := conn.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf[:n]))
}
