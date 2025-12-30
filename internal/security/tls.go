// Package security provides TLS and mTLS for NebulaIO
package security

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/piwi3910/nebulaio/internal/config"
	"github.com/rs/zerolog/log"
)

// TLSManager handles TLS certificate management with auto-generation support.
type TLSManager struct {
	config    *config.TLSConfig
	tlsConfig *tls.Config
	certFile  string
	keyFile   string
	caFile    string
}

// NewTLSManager creates a new TLS manager with the given configuration
// It will automatically generate self-signed certificates if needed.
func NewTLSManager(cfg *config.TLSConfig, hostname string) (*TLSManager, error) {
	if cfg == nil {
		return nil, errors.New("TLS configuration is nil")
	}

	m := &TLSManager{
		config: cfg,
	}

	// Ensure cert directory exists
	err := os.MkdirAll(cfg.CertDir, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	// Determine certificate paths
	switch {
	case cfg.CertFile != "" && cfg.KeyFile != "":
		// Use provided certificates
		m.certFile = cfg.CertFile
		m.keyFile = cfg.KeyFile
		m.caFile = cfg.CAFile
		log.Info().
			Str("cert_file", cfg.CertFile).
			Str("key_file", cfg.KeyFile).
			Msg("Using provided TLS certificates")
	case cfg.AutoGenerate:
		// Auto-generate certificates
		m.certFile = filepath.Join(cfg.CertDir, "server.crt")
		m.keyFile = filepath.Join(cfg.CertDir, "server.key")
		m.caFile = filepath.Join(cfg.CertDir, "ca.crt")

		err := m.ensureCertificates(hostname)
		if err != nil {
			return nil, fmt.Errorf("failed to generate certificates: %w", err)
		}
	default:
		return nil, errors.New("TLS enabled but no certificates provided and auto_generate is disabled")
	}

	// Build TLS configuration
	tlsConfig, err := m.buildTLSConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build TLS config: %w", err)
	}

	m.tlsConfig = tlsConfig

	return m, nil
}

// GetTLSConfig returns the TLS configuration for use with HTTP servers.
func (m *TLSManager) GetTLSConfig() *tls.Config {
	return m.tlsConfig
}

// GetCertFile returns the path to the server certificate.
func (m *TLSManager) GetCertFile() string {
	return m.certFile
}

// GetKeyFile returns the path to the server private key.
func (m *TLSManager) GetKeyFile() string {
	return m.keyFile
}

// GetCAFile returns the path to the CA certificate.
func (m *TLSManager) GetCAFile() string {
	return m.caFile
}

// ensureCertificates checks if certificates exist and generates them if not.
func (m *TLSManager) ensureCertificates(hostname string) error {
	// Check if certificates already exist
	certExists := fileExists(m.certFile) && fileExists(m.keyFile)
	if certExists {
		// Verify certificate is still valid
		valid, err := m.verifyCertificate()
		if err != nil {
			log.Warn().Err(err).Msg("Failed to verify existing certificate, regenerating")

			certExists = false
		} else if !valid {
			log.Info().Msg("Existing certificate expired or invalid, regenerating")

			certExists = false
		}
	}

	if certExists {
		log.Info().
			Str("cert_file", m.certFile).
			Msg("Using existing TLS certificates")

		return nil
	}

	// Generate new certificates
	log.Info().Msg("Generating self-signed TLS certificates")

	return m.generateCertificates(hostname)
}

// verifyCertificate checks if the existing certificate is valid.
func (m *TLSManager) verifyCertificate() (bool, error) {
	certPEM, err := os.ReadFile(m.certFile)
	if err != nil {
		return false, err
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return false, errors.New("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}

	// Check if certificate is still valid (with 30-day buffer)
	renewBefore := time.Now().Add(30 * 24 * time.Hour)
	if cert.NotAfter.Before(renewBefore) {
		return false, nil
	}

	return true, nil
}

// generateCertificates creates a CA and server certificate.
func (m *TLSManager) generateCertificates(hostname string) error {
	// Generate CA first
	caCert, caKey, err := m.generateCA()
	if err != nil {
		return fmt.Errorf("failed to generate CA: %w", err)
	}

	// Save CA certificate
	err = m.saveCertificate(m.caFile, caCert)
	if err != nil {
		return fmt.Errorf("failed to save CA certificate: %w", err)
	}

	// Generate server certificate
	serverCert, serverKey, err := m.generateServerCert(caCert, caKey, hostname)
	if err != nil {
		return fmt.Errorf("failed to generate server certificate: %w", err)
	}

	// Save server certificate and key
	err = m.saveCertificate(m.certFile, serverCert)
	if err != nil {
		return fmt.Errorf("failed to save server certificate: %w", err)
	}

	err = m.saveKey(m.keyFile, serverKey)
	if err != nil {
		return fmt.Errorf("failed to save server key: %w", err)
	}

	// Also save CA key for potential future use (cluster operations)
	caKeyFile := filepath.Join(m.config.CertDir, "ca.key")
	err = m.saveKey(caKeyFile, caKey)
	if err != nil {
		return fmt.Errorf("failed to save CA key: %w", err)
	}

	log.Info().
		Str("ca_file", m.caFile).
		Str("cert_file", m.certFile).
		Str("key_file", m.keyFile).
		Int("validity_days", m.config.ValidityDays).
		Msg("Generated self-signed TLS certificates")

	return nil
}

// generateCA creates a self-signed CA certificate.
func (m *TLSManager) generateCA() ([]byte, *ecdsa.PrivateKey, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	// CA is valid for 10 years
	caValidity := 10 * 365 * 24 * time.Hour

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{m.config.Organization},
			CommonName:   m.config.Organization + " CA",
		},
		NotBefore:             now,
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return nil, nil, err
	}

	return certDER, privateKey, nil
}

// generateServerCert creates a server certificate signed by the CA.
func (m *TLSManager) generateServerCert(caCertDER []byte, caKey *ecdsa.PrivateKey, hostname string) ([]byte, *ecdsa.PrivateKey, error) {
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		return nil, nil, err
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, nil, err
	}

	now := time.Now()
	validity := time.Duration(m.config.ValidityDays) * 24 * time.Hour

	// Build DNS names
	dnsNames := []string{"localhost", hostname}
	dnsNames = append(dnsNames, m.config.DNSNames...)

	// Build IP addresses
	ipAddresses := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}

	// Add configured IP addresses
	for _, ipStr := range m.config.IPAddresses {
		if ip := net.ParseIP(ipStr); ip != nil {
			ipAddresses = append(ipAddresses, ip)
		}
	}

	// Detect local IP addresses
	localIPs := getLocalIPs()
	ipAddresses = append(ipAddresses, localIPs...)

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{m.config.Organization},
			CommonName:   hostname,
		},
		NotBefore:             now,
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &privateKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	return certDER, privateKey, nil
}

// saveCertificate saves a DER-encoded certificate to a PEM file.
func (m *TLSManager) saveCertificate(path string, certDER []byte) error {
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	return os.WriteFile(path, certPEM, 0644)
}

// saveKey saves an ECDSA private key to a PEM file.
func (m *TLSManager) saveKey(path string, key *ecdsa.PrivateKey) error {
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	return os.WriteFile(path, keyPEM, 0600)
}

// buildTLSConfig creates a tls.Config from the manager's settings.
func (m *TLSManager) buildTLSConfig() (*tls.Config, error) {
	// Load server certificate
	cert, err := tls.LoadX509KeyPair(m.certFile, m.keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	// Determine minimum TLS version - G402: Always TLS 1.2 or higher for security
	var minVersion uint16 = tls.VersionTLS12
	if m.config.MinVersion == "1.3" {
		minVersion = tls.VersionTLS13
	}

	// Build client auth mode
	clientAuth := tls.NoClientCert
	if m.config.RequireClientCert {
		clientAuth = tls.RequireAndVerifyClientCert
	}

	// Load CA for client verification if mTLS is enabled
	var clientCAs *x509.CertPool

	if m.config.RequireClientCert && m.caFile != "" {
		caCert, err := os.ReadFile(m.caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		clientCAs = x509.NewCertPool()
		if !clientCAs.AppendCertsFromPEM(caCert) {
			return nil, errors.New("failed to parse CA certificate")
		}
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   minVersion, // TLS 1.2+ enforced, constants are already uint16
		ClientAuth:   clientAuth,
		ClientCAs:    clientCAs,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	return tlsConfig, nil
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getLocalIPs returns all non-loopback local IP addresses.
func getLocalIPs() []net.IP {
	var ips []net.IP

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ips
	}

	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
			if ipNet.IP.To4() != nil || ipNet.IP.To16() != nil {
				ips = append(ips, ipNet.IP)
			}
		}
	}

	return ips
}

// generateSerialNumber is defined in mtls.go
