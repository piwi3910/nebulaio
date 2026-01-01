// Package vault implements HashiCorp Vault integration for NebulaIO KMS.
//
// The Vault provider uses Vault's Transit secrets engine for cryptographic
// operations, providing enterprise-grade key management:
//
//   - Hardware Security Module (HSM) backing
//   - Automatic key rotation
//   - Detailed audit logging
//   - Access policy enforcement
//
// Required Vault configuration:
//   - Transit secrets engine enabled
//   - A named encryption key
//   - Service token with encrypt/decrypt permissions
//
// Example Vault policy:
//
//	path "transit/encrypt/nebulaio" {
//	  capabilities = ["create", "update"]
//	}
//	path "transit/decrypt/nebulaio" {
//	  capabilities = ["create", "update"]
//	}
package vault

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/kms"
)

// Vault key type constants.
const (
	keyTypeAES256GCM96 = "aes256-gcm96"
)

// Config holds Vault KMS provider configuration.
type Config struct {
	kms.ProviderConfig `yaml:",inline"`

	Address       string        `json:"address"                   yaml:"address"`
	Token         string        `json:"-"                         yaml:"token"`
	Namespace     string        `json:"namespace,omitempty"       yaml:"namespace,omitempty"`
	MountPath     string        `json:"mount_path"                yaml:"mount_path"`
	TLSCACert     string        `json:"tls_ca_cert,omitempty"     yaml:"tls_ca_cert,omitempty"`
	TLSClientCert string        `json:"tls_client_cert,omitempty" yaml:"tls_client_cert,omitempty"`
	TLSClientKey  string        `json:"tls_client_key,omitempty"  yaml:"tls_client_key,omitempty"`
	Timeout       time.Duration `json:"timeout,omitempty"         yaml:"timeout,omitempty"`
	MaxRetries    int           `json:"max_retries,omitempty"     yaml:"max_retries,omitempty"`
	TLSSkipVerify bool          `json:"tls_skip_verify,omitempty" yaml:"tls_skip_verify,omitempty"`
}

// DefaultConfig returns a default Vault KMS configuration.
func DefaultConfig() Config {
	// Use VAULT_ADDR environment variable if set, otherwise use default
	vaultAddr := os.Getenv("VAULT_ADDR")
	if vaultAddr == "" {
		vaultAddr = "http://127.0.0.1:8200"
	}

	return Config{
		ProviderConfig: kms.ProviderConfig{
			Type:        "vault",
			Enabled:     true,
			KeyCacheTTL: 5 * time.Minute,
		},
		Address:    vaultAddr,
		MountPath:  "transit",
		Timeout:    30 * time.Second,
		MaxRetries: 3,
	}
}

// VaultClient is the interface for Vault API operations
// This allows for mocking in tests.
type VaultClient interface {
	// Transit engine operations
	CreateKey(ctx context.Context, name string, keyType string) error
	ReadKey(ctx context.Context, name string) (*KeyMetadata, error)
	ListKeys(ctx context.Context) ([]string, error)
	DeleteKey(ctx context.Context, name string) error
	RotateKey(ctx context.Context, name string) error
	Encrypt(ctx context.Context, name string, plaintext []byte) (string, error)
	Decrypt(ctx context.Context, name string, ciphertext string) ([]byte, error)
	GenerateDataKey(ctx context.Context, name string, bits int) (plaintext, ciphertext []byte, err error)
	DecryptDataKey(ctx context.Context, name string, ciphertext []byte) ([]byte, error)
}

// KeyMetadata holds Vault transit key metadata.
type KeyMetadata struct {
	Keys                 map[string]int64
	Name                 string
	Type                 string
	LatestVersion        int
	MinDecryptionVersion int
	MinEncryptionVersion int
	DeletionAllowed      bool
	Derived              bool
	Exportable           bool
	AllowPlaintextBackup bool
}

// Provider implements the KMS Provider interface using HashiCorp Vault Transit.
type Provider struct {
	client VaultClient
	cache  map[string]*cachedKey
	config Config
	mu     sync.RWMutex
	closed bool
}

// cachedKey holds cached key information.
type cachedKey struct {
	// 8-byte fields
	expiresAt time.Time
	// Structs
	info kms.KeyInfo
}

// NewProvider creates a new Vault KMS provider.
func NewProvider(config Config, client VaultClient) (*Provider, error) {
	if config.Address == "" {
		return nil, errors.New("vault address is required")
	}

	if config.MountPath == "" {
		return nil, errors.New("vault mount path is required")
	}

	if client == nil {
		return nil, errors.New("vault client is required")
	}

	return &Provider{
		config: config,
		client: client,
		cache:  make(map[string]*cachedKey),
	}, nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "vault"
}

// GenerateDataKey generates a new data encryption key.
func (p *Provider) GenerateDataKey(ctx context.Context, keyID string, keySpec kms.KeySpec) (*kms.DataKey, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	p.mu.RUnlock()

	// Determine bits based on algorithm
	bits := 256

	switch keySpec.Algorithm {
	case kms.AlgorithmAES128:
		bits = 128
	case kms.AlgorithmAES256, kms.AlgorithmAES256GCM, kms.AlgorithmChaCha20:
		bits = 256
	}

	plaintext, ciphertext, err := p.client.GenerateDataKey(ctx, keyID, bits)
	if err != nil {
		return nil, &kms.WrapError{Op: "GenerateDataKey", KeyID: keyID, Err: err}
	}

	return &kms.DataKey{
		KeyID:      keyID,
		Plaintext:  plaintext,
		Ciphertext: ciphertext,
		Algorithm:  keySpec.Algorithm,
	}, nil
}

// DecryptDataKey decrypts a previously encrypted data key.
func (p *Provider) DecryptDataKey(ctx context.Context, keyID string, encryptedKey []byte) ([]byte, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	p.mu.RUnlock()

	plaintext, err := p.client.DecryptDataKey(ctx, keyID, encryptedKey)
	if err != nil {
		return nil, &kms.WrapError{Op: "DecryptDataKey", KeyID: keyID, Err: err}
	}

	return plaintext, nil
}

// Encrypt encrypts data using the specified key.
func (p *Provider) Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	p.mu.RUnlock()

	ciphertext, err := p.client.Encrypt(ctx, keyID, plaintext)
	if err != nil {
		return nil, &kms.WrapError{Op: "Encrypt", KeyID: keyID, Err: err}
	}

	return []byte(ciphertext), nil
}

// Decrypt decrypts data using the specified key.
func (p *Provider) Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	p.mu.RUnlock()

	plaintext, err := p.client.Decrypt(ctx, keyID, string(ciphertext))
	if err != nil {
		return nil, &kms.WrapError{Op: "Decrypt", KeyID: keyID, Err: err}
	}

	return plaintext, nil
}

// ListKeys returns available encryption keys.
func (p *Provider) ListKeys(ctx context.Context) ([]kms.KeyInfo, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	p.mu.RUnlock()

	keyNames, err := p.client.ListKeys(ctx)
	if err != nil {
		return nil, &kms.WrapError{Op: "ListKeys", Err: err}
	}

	keys := make([]kms.KeyInfo, 0, len(keyNames))
	for _, name := range keyNames {
		info, err := p.GetKeyInfo(ctx, name)
		if err != nil {
			continue // Skip keys we can't read
		}

		keys = append(keys, *info)
	}

	return keys, nil
}

// GetKeyInfo returns information about a specific key.
func (p *Provider) GetKeyInfo(ctx context.Context, keyID string) (*kms.KeyInfo, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	// Check cache
	if cached, ok := p.cache[keyID]; ok && time.Now().Before(cached.expiresAt) {
		p.mu.RUnlock()

		info := cached.info

		return &info, nil
	}

	p.mu.RUnlock()

	// Fetch from Vault
	meta, err := p.client.ReadKey(ctx, keyID)
	if err != nil {
		return nil, &kms.WrapError{Op: "GetKeyInfo", KeyID: keyID, Err: err}
	}

	info := p.metadataToKeyInfo(keyID, meta)

	// Update cache
	p.mu.Lock()
	p.cache[keyID] = &cachedKey{
		info:      *info,
		expiresAt: time.Now().Add(p.config.KeyCacheTTL),
	}
	p.mu.Unlock()

	return info, nil
}

// CreateKey creates a new encryption key.
func (p *Provider) CreateKey(ctx context.Context, spec kms.KeySpec) (*kms.KeyInfo, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	p.mu.RUnlock()

	// Map algorithm to Vault key type
	vaultKeyType := algorithmToVaultType(spec.Algorithm)

	err := p.client.CreateKey(ctx, spec.Name, vaultKeyType)
	if err != nil {
		return nil, &kms.WrapError{Op: "CreateKey", Err: err}
	}

	// Read back the key info
	return p.GetKeyInfo(ctx, spec.Name)
}

// RotateKey rotates an encryption key.
func (p *Provider) RotateKey(ctx context.Context, keyID string) (*kms.KeyInfo, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	p.mu.RUnlock()

	err := p.client.RotateKey(ctx, keyID)
	if err != nil {
		return nil, &kms.WrapError{Op: "RotateKey", KeyID: keyID, Err: err}
	}

	// Invalidate cache
	p.mu.Lock()
	delete(p.cache, keyID)
	p.mu.Unlock()

	// Read back updated info
	return p.GetKeyInfo(ctx, keyID)
}

// DeleteKey schedules a key for deletion.
func (p *Provider) DeleteKey(ctx context.Context, keyID string) error {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return kms.ErrProviderClosed
	}

	p.mu.RUnlock()

	err := p.client.DeleteKey(ctx, keyID)
	if err != nil {
		return &kms.WrapError{Op: "DeleteKey", KeyID: keyID, Err: err}
	}

	// Remove from cache
	p.mu.Lock()
	delete(p.cache, keyID)
	p.mu.Unlock()

	return nil
}

// Close closes the provider.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	p.cache = nil

	return nil
}

// metadataToKeyInfo converts Vault key metadata to kms.KeyInfo.
func (p *Provider) metadataToKeyInfo(keyID string, meta *KeyMetadata) *kms.KeyInfo {
	var createdAt time.Time

	if len(meta.Keys) > 0 {
		// Get the earliest key version timestamp
		for _, ts := range meta.Keys {
			t := time.Unix(ts, 0)
			if createdAt.IsZero() || t.Before(createdAt) {
				createdAt = t
			}
		}
	}

	var rotatedAt *time.Time

	if meta.LatestVersion > 1 && len(meta.Keys) > 0 {
		// Get the latest key version timestamp
		latestKey := strconv.Itoa(meta.LatestVersion)
		if ts, ok := meta.Keys[latestKey]; ok {
			t := time.Unix(ts, 0)
			rotatedAt = &t
		}
	}

	return &kms.KeyInfo{
		KeyID:     keyID,
		Name:      meta.Name,
		Algorithm: vaultTypeToAlgorithm(meta.Type),
		Usage:     kms.KeyUsageEncrypt,
		State:     kms.KeyStateEnabled,
		CreatedAt: createdAt,
		RotatedAt: rotatedAt,
	}
}

// algorithmToVaultType maps kms.Algorithm to Vault key type.
func algorithmToVaultType(algo kms.Algorithm) string {
	switch algo {
	case kms.AlgorithmAES128:
		return "aes128-gcm96"
	case kms.AlgorithmAES256, kms.AlgorithmAES256GCM:
		return keyTypeAES256GCM96
	case kms.AlgorithmChaCha20:
		return "chacha20-poly1305"
	case kms.AlgorithmRSA2048:
		return "rsa-2048"
	case kms.AlgorithmRSA3072:
		return "rsa-3072"
	case kms.AlgorithmRSA4096:
		return "rsa-4096"
	case kms.AlgorithmECDSAP256:
		return "ecdsa-p256"
	case kms.AlgorithmECDSAP384:
		return "ecdsa-p384"
	default:
		return keyTypeAES256GCM96
	}
}

// vaultTypeToAlgorithm maps Vault key type to kms.Algorithm.
func vaultTypeToAlgorithm(vaultType string) kms.Algorithm {
	switch vaultType {
	case "aes128-gcm96":
		return kms.AlgorithmAES128
	case keyTypeAES256GCM96:
		return kms.AlgorithmAES256GCM
	case "chacha20-poly1305":
		return kms.AlgorithmChaCha20
	case "rsa-2048":
		return kms.AlgorithmRSA2048
	case "rsa-3072":
		return kms.AlgorithmRSA3072
	case "rsa-4096":
		return kms.AlgorithmRSA4096
	case "ecdsa-p256":
		return kms.AlgorithmECDSAP256
	case "ecdsa-p384":
		return kms.AlgorithmECDSAP384
	default:
		return kms.AlgorithmAES256GCM
	}
}

// Ensure Provider implements kms.Provider.
var _ kms.Provider = (*Provider)(nil)

// MockVaultClient is a mock implementation for testing.
type MockVaultClient struct {
	keys map[string]*KeyMetadata
	data map[string]map[string][]byte // keyName -> ciphertext -> plaintext
	mu   sync.RWMutex
}

// NewMockVaultClient creates a new mock Vault client.
func NewMockVaultClient() *MockVaultClient {
	return &MockVaultClient{
		keys: make(map[string]*KeyMetadata),
		data: make(map[string]map[string][]byte),
	}
}

// CreateKey creates a new key in the mock.
func (m *MockVaultClient) CreateKey(ctx context.Context, name string, keyType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.keys[name] = &KeyMetadata{
		Name:          name,
		Type:          keyType,
		LatestVersion: 1,
		Keys:          map[string]int64{"1": time.Now().Unix()},
	}
	m.data[name] = make(map[string][]byte)

	return nil
}

// ReadKey reads key metadata from the mock.
func (m *MockVaultClient) ReadKey(ctx context.Context, name string) (*KeyMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	meta, ok := m.keys[name]
	if !ok {
		return nil, errors.New("key not found")
	}

	return meta, nil
}

// ListKeys lists all keys in the mock.
func (m *MockVaultClient) ListKeys(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.keys))
	for name := range m.keys {
		names = append(names, name)
	}

	return names, nil
}

// DeleteKey deletes a key from the mock.
func (m *MockVaultClient) DeleteKey(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.keys, name)
	delete(m.data, name)

	return nil
}

// RotateKey rotates a key in the mock.
func (m *MockVaultClient) RotateKey(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.keys[name]
	if !ok {
		return errors.New("key not found")
	}

	meta.LatestVersion++
	meta.Keys[strconv.Itoa(meta.LatestVersion)] = time.Now().Unix()

	return nil
}

// Encrypt encrypts data in the mock.
func (m *MockVaultClient) Encrypt(ctx context.Context, name string, plaintext []byte) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.keys[name]; !ok {
		return "", errors.New("key not found")
	}

	// Simple mock encryption: base64 encode
	ciphertext := "vault:v1:" + base64.StdEncoding.EncodeToString(plaintext)

	if m.data[name] == nil {
		m.data[name] = make(map[string][]byte)
	}

	m.data[name][ciphertext] = plaintext

	return ciphertext, nil
}

// Decrypt decrypts data in the mock.
func (m *MockVaultClient) Decrypt(ctx context.Context, name string, ciphertext string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.keys[name]; !ok {
		return nil, errors.New("key not found")
	}

	plaintext, ok := m.data[name][ciphertext]
	if !ok {
		return nil, errors.New("decryption failed")
	}

	return plaintext, nil
}

// GenerateDataKey generates a data key in the mock.
func (m *MockVaultClient) GenerateDataKey(ctx context.Context, name string, bits int) (plaintext, ciphertext []byte, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.keys[name]; !ok {
		return nil, nil, errors.New("key not found")
	}

	// Generate random plaintext
	plaintext = make([]byte, bits/8)
	for i := range plaintext {
		plaintext[i] = byte(i)
	}

	// Mock ciphertext
	ciphertext = []byte("vault:v1:" + base64.StdEncoding.EncodeToString(plaintext))

	if m.data[name] == nil {
		m.data[name] = make(map[string][]byte)
	}

	m.data[name][string(ciphertext)] = plaintext

	return plaintext, ciphertext, nil
}

// DecryptDataKey decrypts a data key in the mock.
func (m *MockVaultClient) DecryptDataKey(ctx context.Context, name string, ciphertext []byte) ([]byte, error) {
	return m.Decrypt(ctx, name, string(ciphertext))
}

// Ensure MockVaultClient implements VaultClient.
var _ VaultClient = (*MockVaultClient)(nil)
