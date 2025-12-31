// Package security provides mTLS (mutual TLS) for internal communication in NebulaIO
package security

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

// ClientCertContextKey is the context key for client certificates.
const ClientCertContextKey contextKey = "client_cert"

// CertificateType represents the type of certificate.
type CertificateType string

// File permission constants.
const (
	certDirPermissions   = 0700 // Directory permissions for cert storage
	certFilePermissions  = 0644 // Public cert file permissions
	keyFilePermissions   = 0600 // Private key file permissions
)

const (
	CertTypeCA     CertificateType = "CA"
	CertTypeServer CertificateType = "SERVER"
	CertTypeClient CertificateType = "CLIENT"
	CertTypePeer   CertificateType = "PEER" // Both server and client
)

// CertificateInfo contains information about a certificate.
type CertificateInfo struct {
	NotBefore     time.Time          `json:"not_before"`
	NotAfter      time.Time          `json:"not_after"`
	RevokedAt     *time.Time         `json:"revoked_at,omitempty"`
	Fingerprint   string             `json:"fingerprint"`
	Type          CertificateType    `json:"type"`
	CommonName    string             `json:"common_name"`
	RevokedReason string             `json:"revoked_reason,omitempty"`
	ID            string             `json:"id"`
	SerialNumber  string             `json:"serial_number"`
	IssuedBy      string             `json:"issued_by"`
	DNSNames      []string           `json:"dns_names"`
	ExtKeyUsage   []x509.ExtKeyUsage `json:"ext_key_usage"`
	IPAddresses   []net.IP           `json:"ip_addresses"`
	Organization  []string           `json:"organization"`
	KeyUsage      x509.KeyUsage      `json:"key_usage"`
	IsRevoked     bool               `json:"is_revoked"`
}

// CertificateBundle contains a certificate and its private key.
type CertificateBundle struct {
	PrivateKey     crypto.PrivateKey
	Certificate    *x509.Certificate
	Info           *CertificateInfo
	CertificatePEM []byte
	PrivateKeyPEM  []byte
}

// MTLSConfig contains configuration for mTLS.
type MTLSConfig struct {
	CertDir              string        `json:"cert_dir"`
	KeyAlgorithm         string        `json:"key_algorithm"`
	Organization         []string      `json:"organization"`
	CAValidityDuration   time.Duration `json:"ca_validity_duration"`
	CertValidityDuration time.Duration `json:"cert_validity_duration"`
	AutoRenewBefore      time.Duration `json:"auto_renew_before"`
	MinTLSVersion        uint16        `json:"min_tls_version"`
	EnableOCSP           bool          `json:"enable_ocsp"`
	EnableCRL            bool          `json:"enable_crl"`
	RequireClientCert    bool          `json:"require_client_cert"`
}

// MTLSManager manages mTLS certificates and connections.
type MTLSManager struct {
	storage         CertificateStorage
	config          *MTLSConfig
	caCert          *CertificateBundle
	certificates    map[string]*CertificateBundle
	revokedCerts    map[string]*CertificateInfo
	certPool        *x509.CertPool
	renewalCallback func(bundle *CertificateBundle)
	stopChan        chan struct{}
	wg              sync.WaitGroup
	mu              sync.RWMutex
}

// CertificateStorage defines the interface for certificate persistence.
type CertificateStorage interface {
	StoreCA(ctx context.Context, bundle *CertificateBundle) error
	LoadCA(ctx context.Context) (*CertificateBundle, error)
	StoreCertificate(ctx context.Context, bundle *CertificateBundle) error
	LoadCertificate(ctx context.Context, id string) (*CertificateBundle, error)
	ListCertificates(ctx context.Context) ([]*CertificateInfo, error)
	StoreRevocation(ctx context.Context, info *CertificateInfo) error
	GetRevocations(ctx context.Context) ([]*CertificateInfo, error)
}

// NewMTLSManager creates a new mTLS manager.
func NewMTLSManager(config *MTLSConfig, storage CertificateStorage) (*MTLSManager, error) {
	if config == nil {
		config = &MTLSConfig{
			CertDir:              "./certs",
			CAValidityDuration:   10 * 365 * 24 * time.Hour, // 10 years
			CertValidityDuration: 365 * 24 * time.Hour,      // 1 year
			KeyAlgorithm:         "ECDSA-P256",
			Organization:         []string{"NebulaIO"},
			AutoRenewBefore:      30 * 24 * time.Hour, // 30 days
			EnableOCSP:           false,
			EnableCRL:            true,
			RequireClientCert:    true,
			MinTLSVersion:        tls.VersionTLS12,
		}
	}

	// Create cert directory if needed
	if config.CertDir != "" {
		err := os.MkdirAll(config.CertDir, certDirPermissions)
		if err != nil {
			return nil, fmt.Errorf("failed to create cert directory: %w", err)
		}
	}

	m := &MTLSManager{
		config:       config,
		certificates: make(map[string]*CertificateBundle),
		revokedCerts: make(map[string]*CertificateInfo),
		certPool:     x509.NewCertPool(),
		storage:      storage,
		stopChan:     make(chan struct{}),
	}

	// Try to load existing CA
	if storage != nil {
		ca, err := storage.LoadCA(context.Background())
		if err == nil && ca != nil {
			m.caCert = ca
			m.certPool.AddCert(ca.Certificate)
		}
	}

	// Start renewal checker
	m.wg.Add(1)

	go m.renewalChecker()

	return m, nil
}

// InitializeCA initializes or loads the Certificate Authority.
func (m *MTLSManager) InitializeCA(ctx context.Context, commonName string) (*CertificateBundle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if CA already exists
	if m.caCert != nil {
		return m.caCert, nil
	}

	// Try to load existing CA
	if ca := m.tryLoadExistingCA(ctx); ca != nil {
		m.caCert = ca
		m.certPool.AddCert(ca.Certificate)
		return ca, nil
	}

	// Generate new CA
	bundle, err := m.generateNewCA(commonName)
	if err != nil {
		return nil, err
	}

	m.caCert = bundle
	m.certPool.AddCert(bundle.Certificate)

	// Persist CA to disk and storage
	if err := m.persistCA(ctx, bundle); err != nil {
		return nil, err
	}

	return bundle, nil
}

// tryLoadExistingCA attempts to load CA from storage or disk.
func (m *MTLSManager) tryLoadExistingCA(ctx context.Context) *CertificateBundle {
	// Try to load from storage
	if m.storage != nil {
		if ca, err := m.storage.LoadCA(ctx); err == nil && ca != nil {
			return ca
		}
	}

	// Try to load from disk
	caCertPath := filepath.Join(m.config.CertDir, "ca.crt")
	caKeyPath := filepath.Join(m.config.CertDir, "ca.key")

	if _, err := os.Stat(caCertPath); err == nil {
		if ca, err := m.loadCertificateFromDisk(caCertPath, caKeyPath); err == nil {
			return ca
		}
	}

	return nil
}

// generateNewCA generates a new CA certificate and key.
func (m *MTLSManager) generateNewCA(commonName string) (*CertificateBundle, error) {
	privateKey, err := m.generatePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate CA private key: %w", err)
	}

	now := time.Now()
	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: m.config.Organization,
		},
		NotBefore:             now,
		NotAfter:              now.Add(m.config.CAValidityDuration),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, publicKey(privateKey), privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CA certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM, err := encodePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	return &CertificateBundle{
		Certificate:    cert,
		PrivateKey:     privateKey,
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
		Info: &CertificateInfo{
			ID:           uuid.New().String(),
			Type:         CertTypeCA,
			CommonName:   commonName,
			Organization: m.config.Organization,
			SerialNumber: serialNumber.String(),
			NotBefore:    now,
			NotAfter:     now.Add(m.config.CAValidityDuration),
			Fingerprint:  fingerprintCert(cert),
			KeyUsage:     template.KeyUsage,
		},
	}, nil
}

// persistCA saves CA to disk and storage.
func (m *MTLSManager) persistCA(ctx context.Context, bundle *CertificateBundle) error {
	caCertPath := filepath.Join(m.config.CertDir, "ca.crt")
	caKeyPath := filepath.Join(m.config.CertDir, "ca.key")

	if err := os.WriteFile(caCertPath, bundle.CertificatePEM, certFilePermissions); err != nil {
		return err
	}

	if err := os.WriteFile(caKeyPath, bundle.PrivateKeyPEM, keyFilePermissions); err != nil {
		return err
	}

	if m.storage != nil {
		if err := m.storage.StoreCA(ctx, bundle); err != nil {
			return err
		}
	}

	return nil
}

// IssueCertificate issues a new certificate signed by the CA.
func (m *MTLSManager) IssueCertificate(ctx context.Context, certType CertificateType, commonName string, dnsNames []string, ipAddresses []net.IP) (*CertificateBundle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.caCert == nil {
		return nil, errors.New("CA not initialized")
	}

	privateKey, err := m.generatePrivateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate private key: %w", err)
	}

	now := time.Now()

	serialNumber, err := generateSerialNumber()
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: m.config.Organization,
		},
		NotBefore:             now,
		NotAfter:              now.Add(m.config.CertValidityDuration),
		DNSNames:              dnsNames,
		IPAddresses:           ipAddresses,
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	// Set key usage based on certificate type
	switch certType {
	case CertTypeServer:
		template.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	case CertTypeClient:
		template.KeyUsage = x509.KeyUsageDigitalSignature
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	case CertTypePeer:
		template.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, m.caCert.Certificate, publicKey(privateKey), m.caCert.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyPEM, err := encodePrivateKey(privateKey)
	if err != nil {
		return nil, err
	}

	bundle := &CertificateBundle{
		Certificate:    cert,
		PrivateKey:     privateKey,
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
		Info: &CertificateInfo{
			ID:           uuid.New().String(),
			Type:         certType,
			CommonName:   commonName,
			Organization: m.config.Organization,
			DNSNames:     dnsNames,
			IPAddresses:  ipAddresses,
			SerialNumber: serialNumber.String(),
			NotBefore:    now,
			NotAfter:     now.Add(m.config.CertValidityDuration),
			IssuedBy:     m.caCert.Info.CommonName,
			Fingerprint:  fingerprintCert(cert),
			KeyUsage:     template.KeyUsage,
			ExtKeyUsage:  template.ExtKeyUsage,
		},
	}

	m.certificates[bundle.Info.ID] = bundle

	// Save to disk
	certPath := filepath.Join(m.config.CertDir, bundle.Info.ID+".crt")
	keyPath := filepath.Join(m.config.CertDir, bundle.Info.ID+".key")

	err = os.WriteFile(certPath, certPEM, certFilePermissions)
	if err != nil {
		return nil, err
	}

	err = os.WriteFile(keyPath, keyPEM, keyFilePermissions)
	if err != nil {
		return nil, err
	}

	// Store in storage
	if m.storage != nil {
		err := m.storage.StoreCertificate(ctx, bundle)
		if err != nil {
			return nil, err
		}
	}

	return bundle, nil
}

// RevokeCertificate revokes a certificate.
func (m *MTLSManager) RevokeCertificate(ctx context.Context, certID string, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	bundle, exists := m.certificates[certID]
	if !exists {
		return fmt.Errorf("certificate not found: %s", certID)
	}

	now := time.Now()
	bundle.Info.IsRevoked = true
	bundle.Info.RevokedAt = &now
	bundle.Info.RevokedReason = reason

	m.revokedCerts[bundle.Info.SerialNumber] = bundle.Info

	if m.storage != nil {
		err := m.storage.StoreRevocation(ctx, bundle.Info)
		if err != nil {
			return err
		}
	}

	return nil
}

// IsRevoked checks if a certificate is revoked.
func (m *MTLSManager) IsRevoked(serialNumber string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, revoked := m.revokedCerts[serialNumber]

	return revoked
}

// GetServerTLSConfig returns a TLS config for servers.
func (m *MTLSManager) GetServerTLSConfig(bundle *CertificateBundle) (*tls.Config, error) {
	if m.caCert == nil {
		return nil, errors.New("CA not initialized")
	}

	cert, err := tls.X509KeyPair(bundle.CertificatePEM, bundle.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}

	clientAuth := tls.RequireAndVerifyClientCert
	if !m.config.RequireClientCert {
		clientAuth = tls.VerifyClientCertIfGiven
	}

	//nolint:gosec // G402: MinTLSVersion is user-configurable, defaults to TLS 1.2
	config := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    m.certPool,
		ClientAuth:   clientAuth,
		MinVersion:   m.config.MinTLSVersion,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
		PreferServerCipherSuites: true,
		VerifyPeerCertificate:    m.verifyPeerCertificate,
	}

	return config, nil
}

// GetClientTLSConfig returns a TLS config for clients.
func (m *MTLSManager) GetClientTLSConfig(bundle *CertificateBundle) (*tls.Config, error) {
	if m.caCert == nil {
		return nil, errors.New("CA not initialized")
	}

	cert, err := tls.X509KeyPair(bundle.CertificatePEM, bundle.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}

	//nolint:gosec // G402: MinTLSVersion is user-configurable, defaults to TLS 1.2
	config := &tls.Config{
		Certificates:          []tls.Certificate{cert},
		RootCAs:               m.certPool,
		MinVersion:            m.config.MinTLSVersion,
		VerifyPeerCertificate: m.verifyPeerCertificate,
	}

	return config, nil
}

// verifyPeerCertificate performs additional certificate verification.
func (m *MTLSManager) verifyPeerCertificate(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return errors.New("no certificates presented")
	}

	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return err
	}

	// Check if certificate is revoked
	if m.IsRevoked(cert.SerialNumber.String()) {
		return errors.New("certificate has been revoked")
	}

	return nil
}

// GetHTTPClient returns an HTTP client configured with mTLS.
func (m *MTLSManager) GetHTTPClient(bundle *CertificateBundle) (*http.Client, error) {
	tlsConfig, err := m.GetClientTLSConfig(bundle)
	if err != nil {
		return nil, err
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		MaxIdleConns:    100,
		IdleConnTimeout: 90 * time.Second,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

// CreateHTTPServer creates an HTTP server with mTLS.
func (m *MTLSManager) CreateHTTPServer(bundle *CertificateBundle, addr string, handler http.Handler) (*http.Server, error) {
	tlsConfig, err := m.GetServerTLSConfig(bundle)
	if err != nil {
		return nil, err
	}

	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 10 * time.Second, // Prevent Slowloris attacks
	}, nil
}

// renewalChecker periodically checks for certificates that need renewal.
func (m *MTLSManager) renewalChecker() {
	defer m.wg.Done()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.checkAndRenewCertificates(context.Background())
		}
	}
}

// checkAndRenewCertificates checks all certificates and renews those expiring soon.
func (m *MTLSManager) checkAndRenewCertificates(ctx context.Context) {
	m.mu.RLock()

	certs := make([]*CertificateBundle, 0, len(m.certificates))
	for _, cert := range m.certificates {
		certs = append(certs, cert)
	}

	m.mu.RUnlock()

	now := time.Now()
	renewBefore := now.Add(m.config.AutoRenewBefore)

	for _, bundle := range certs {
		if bundle.Info.IsRevoked {
			continue
		}

		if bundle.Certificate.NotAfter.Before(renewBefore) {
			// Certificate needs renewal
			newBundle, err := m.IssueCertificate(
				ctx,
				bundle.Info.Type,
				bundle.Info.CommonName,
				bundle.Info.DNSNames,
				bundle.Info.IPAddresses,
			)
			if err != nil {
				continue
			}

			if m.renewalCallback != nil {
				m.renewalCallback(newBundle)
			}
		}
	}
}

// SetRenewalCallback sets a callback for when certificates are renewed.
func (m *MTLSManager) SetRenewalCallback(callback func(bundle *CertificateBundle)) {
	m.renewalCallback = callback
}

// GetCertificate retrieves a certificate by ID.
func (m *MTLSManager) GetCertificate(certID string) (*CertificateBundle, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	bundle, exists := m.certificates[certID]
	if !exists {
		return nil, fmt.Errorf("certificate not found: %s", certID)
	}

	return bundle, nil
}

// ListCertificates returns all certificates.
func (m *MTLSManager) ListCertificates() []*CertificateInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]*CertificateInfo, 0, len(m.certificates))
	for _, bundle := range m.certificates {
		infos = append(infos, bundle.Info)
	}

	return infos
}

// GetCA returns the CA certificate bundle.
func (m *MTLSManager) GetCA() *CertificateBundle {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.caCert
}

// GetCertPool returns the certificate pool containing the CA.
func (m *MTLSManager) GetCertPool() *x509.CertPool {
	return m.certPool
}

// GenerateCRL generates a Certificate Revocation List.
func (m *MTLSManager) GenerateCRL(ctx context.Context) ([]byte, error) {
	if m.caCert == nil {
		return nil, errors.New("CA not initialized")
	}

	m.mu.RLock()

	revokedCerts := make([]pkix.RevokedCertificate, 0, len(m.revokedCerts))
	for _, info := range m.revokedCerts {
		serialNumber := new(big.Int)
		serialNumber.SetString(info.SerialNumber, 10)
		revokedCerts = append(revokedCerts, pkix.RevokedCertificate{
			SerialNumber:   serialNumber,
			RevocationTime: *info.RevokedAt,
		})
	}

	m.mu.RUnlock()

	now := time.Now()
	template := &x509.RevocationList{
		Number:              big.NewInt(1),
		ThisUpdate:          now,
		NextUpdate:          now.Add(24 * time.Hour),
		RevokedCertificates: revokedCerts,
	}

	crlDER, err := x509.CreateRevocationList(rand.Reader, template, m.caCert.Certificate, m.caCert.PrivateKey.(crypto.Signer))
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crlDER}), nil
}

// ExportCertificate exports a certificate and key as PEM files.
func (m *MTLSManager) ExportCertificate(certID string, certPath, keyPath string) error {
	bundle, err := m.GetCertificate(certID)
	if err != nil {
		return err
	}

	err = os.WriteFile(certPath, bundle.CertificatePEM, 0644)
	if err != nil {
		return err
	}

	err = os.WriteFile(keyPath, bundle.PrivateKeyPEM, 0600)
	if err != nil {
		return err
	}

	return nil
}

// loadCertificateFromDisk loads a certificate from disk.
func (m *MTLSManager) loadCertificateFromDisk(certPath, keyPath string) (*CertificateBundle, error) {
	certPEM, keyPEM, err := m.readCertificateFiles(certPath, keyPath)
	if err != nil {
		return nil, err
	}

	cert, err := m.parseCertificate(certPEM)
	if err != nil {
		return nil, err
	}

	privateKey, err := m.parsePrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}

	certType := m.determineCertificateType(cert)

	return &CertificateBundle{
		Certificate:    cert,
		PrivateKey:     privateKey,
		CertificatePEM: certPEM,
		PrivateKeyPEM:  keyPEM,
		Info: &CertificateInfo{
			ID:           uuid.New().String(),
			Type:         certType,
			CommonName:   cert.Subject.CommonName,
			Organization: cert.Subject.Organization,
			DNSNames:     cert.DNSNames,
			IPAddresses:  cert.IPAddresses,
			SerialNumber: cert.SerialNumber.String(),
			NotBefore:    cert.NotBefore,
			NotAfter:     cert.NotAfter,
			Fingerprint:  fingerprintCert(cert),
			KeyUsage:     cert.KeyUsage,
			ExtKeyUsage:  cert.ExtKeyUsage,
		},
	}, nil
}

// readCertificateFiles reads certificate and key files from disk.
func (m *MTLSManager) readCertificateFiles(certPath, keyPath string) ([]byte, []byte, error) {
	//nolint:gosec // G304: certPath comes from trusted configuration
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, nil, err
	}

	//nolint:gosec // G304: keyPath comes from trusted configuration
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, nil, err
	}

	return certPEM, keyPEM, nil
}

// parseCertificate parses a certificate from PEM data.
func (m *MTLSManager) parseCertificate(certPEM []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	return cert, nil
}

// parsePrivateKey parses a private key from PEM data.
func (m *MTLSManager) parsePrivateKey(keyPEM []byte) (*ecdsa.PrivateKey, error) {
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("failed to decode key PEM")
	}

	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		// Try parsing as PKCS8
		key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		privateKey = key.(*ecdsa.PrivateKey)
	}

	return privateKey, nil
}

// determineCertificateType determines the type of certificate based on its properties.
func (m *MTLSManager) determineCertificateType(cert *x509.Certificate) CertificateType {
	if cert.IsCA {
		return CertTypeCA
	}

	if len(cert.ExtKeyUsage) > 0 {
		hasServer := false
		hasClient := false

		for _, usage := range cert.ExtKeyUsage {
			if usage == x509.ExtKeyUsageServerAuth {
				hasServer = true
			}
			if usage == x509.ExtKeyUsageClientAuth {
				hasClient = true
			}
		}

		switch {
		case hasServer && hasClient:
			return CertTypePeer
		case hasServer:
			return CertTypeServer
		case hasClient:
			return CertTypeClient
		}
	}

	return CertificateType("") // Default/unknown type
}

// generatePrivateKey generates a private key based on configuration.
func (m *MTLSManager) generatePrivateKey() (crypto.PrivateKey, error) {
	switch m.config.KeyAlgorithm {
	case "ECDSA-P256":
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "ECDSA-P384":
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	default:
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
}

// generateSerialNumber generates a random serial number for certificates.
func generateSerialNumber() (*big.Int, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, serialNumberLimit)
}

// publicKey extracts the public key from a private key.
func publicKey(priv crypto.PrivateKey) crypto.PublicKey {
	switch k := priv.(type) {
	case *ecdsa.PrivateKey:
		return &k.PublicKey
	default:
		return nil
	}
}

// encodePrivateKey encodes a private key to PEM format.
func encodePrivateKey(priv crypto.PrivateKey) ([]byte, error) {
	switch k := priv.(type) {
	case *ecdsa.PrivateKey:
		keyBytes, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			return nil, err
		}

		return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}), nil
	default:
		return nil, errors.New("unsupported key type")
	}
}

// fingerprintCert generates a fingerprint for a certificate.
func fingerprintCert(cert *x509.Certificate) string {
	return fmt.Sprintf("%X", cert.Raw[:20])
}

// Stop stops the mTLS manager.
func (m *MTLSManager) Stop() {
	close(m.stopChan)
	m.wg.Wait()
}

// ValidateCertificateChain validates a certificate chain.
func (m *MTLSManager) ValidateCertificateChain(certPEM []byte) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("failed to decode certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}

	opts := x509.VerifyOptions{
		Roots:     m.certPool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}

	_, err = cert.Verify(opts)

	return err
}

// MTLSMiddleware returns an HTTP middleware that verifies client certificates.
func (m *MTLSManager) MTLSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			if m.config.RequireClientCert {
				http.Error(w, "Client certificate required", http.StatusUnauthorized)
				return
			}
		} else {
			cert := r.TLS.PeerCertificates[0]

			// Check if certificate is revoked
			if m.IsRevoked(cert.SerialNumber.String()) {
				http.Error(w, "Certificate revoked", http.StatusUnauthorized)
				return
			}

			// Add certificate info to context
			ctx := context.WithValue(r.Context(), ClientCertContextKey, cert)
			r = r.WithContext(ctx)
		}

		next.ServeHTTP(w, r)
	})
}

// PeerCertificateInfo represents information about a peer's certificate.
type PeerCertificateInfo struct {
	NotBefore    time.Time `json:"not_before"`
	NotAfter     time.Time `json:"not_after"`
	CommonName   string    `json:"common_name"`
	SerialNumber string    `json:"serial_number"`
	Fingerprint  string    `json:"fingerprint"`
	Organization []string  `json:"organization"`
	DNSNames     []string  `json:"dns_names"`
}

// GetPeerCertificateInfo extracts certificate info from an HTTP request.
func GetPeerCertificateInfo(r *http.Request) *PeerCertificateInfo {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return nil
	}

	cert := r.TLS.PeerCertificates[0]

	return &PeerCertificateInfo{
		CommonName:   cert.Subject.CommonName,
		Organization: cert.Subject.Organization,
		SerialNumber: cert.SerialNumber.String(),
		NotBefore:    cert.NotBefore,
		NotAfter:     cert.NotAfter,
		DNSNames:     cert.DNSNames,
		Fingerprint:  fingerprintCert(cert),
	}
}
