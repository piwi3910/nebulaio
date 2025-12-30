// Package local implements a file-based KMS provider for development and testing.
//
// The local KMS provider stores encryption keys in a file on the local
// filesystem. It is suitable for:
//
//   - Development and testing environments
//   - Single-node deployments without external KMS
//   - Evaluation before deploying production KMS
//
// WARNING: The local provider stores keys unencrypted on disk. For production
// environments, use a proper KMS like HashiCorp Vault, AWS KMS, or Azure Key Vault.
//
// Keys are stored in JSON format at the configured path:
//
//	{
//	  "master_key": "base64-encoded-key",
//	  "data_keys": {
//	    "key-id": "encrypted-dek"
//	  }
//	}
package local

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/kms"
	"golang.org/x/crypto/pbkdf2"
)

// Config holds local KMS provider configuration.
type Config struct {
	KeyStorePath       string `json:"keyStorePath" yaml:"keyStorePath"`
	MasterKeyFile      string `json:"masterKeyFile,omitempty" yaml:"masterKeyFile,omitempty"`
	MasterPassword     string `json:"-" yaml:"masterPassword,omitempty"`
	KeyDerivationSalt  string `json:"keyDerivationSalt,omitempty" yaml:"keyDerivationSalt,omitempty"`
	kms.ProviderConfig `yaml:",inline"`
}

// DefaultConfig returns a default local KMS configuration.
func DefaultConfig() Config {
	return Config{
		ProviderConfig: kms.ProviderConfig{
			Type:        "local",
			Enabled:     true,
			KeyCacheTTL: 5 * time.Minute,
		},
		KeyStorePath: "./data/kms/keys",
	}
}

// Provider implements the KMS Provider interface using local file storage
// This is primarily for development and testing - NOT for production use.
type Provider struct {
	keys      map[string]*storedKey
	config    Config
	masterKey []byte
	mu        sync.RWMutex
	closed    bool
}

// storedKey represents a key stored on disk.
type storedKey struct {
	Info          kms.KeyInfo `json:"info"`
	Material      []byte      `json:"material"` // Encrypted key material
	PlainMaterial []byte      `json:"-"`        // Decrypted key material (cached)
}

// NewProvider creates a new local KMS provider.
func NewProvider(config Config) (*Provider, error) {
	if config.KeyStorePath == "" {
		return nil, errors.New("keyStorePath is required")
	}

	// Ensure key store directory exists
	err := os.MkdirAll(config.KeyStorePath, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to create key store directory: %w", err)
	}

	p := &Provider{
		config: config,
		keys:   make(map[string]*storedKey),
	}

	// Initialize master key
	err = p.initMasterKey()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize master key: %w", err)
	}

	// Load existing keys
	err = p.loadKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to load keys: %w", err)
	}

	return p, nil
}

// initMasterKey initializes the master key from file, password, or generates new.
func (p *Provider) initMasterKey() error {
	// Option 1: Load from file
	if p.config.MasterKeyFile != "" {
		data, err := os.ReadFile(p.config.MasterKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read master key file: %w", err)
		}

		p.masterKey = data

		return nil
	}

	// Option 2: Derive from password
	if p.config.MasterPassword != "" {
		salt := []byte(p.config.KeyDerivationSalt)
		if len(salt) == 0 {
			salt = []byte("nebulaio-local-kms-default-salt")
		}

		p.masterKey = pbkdf2.Key([]byte(p.config.MasterPassword), salt, 100000, 32, sha256.New)

		return nil
	}

	// Option 3: Generate new master key and save it
	masterKeyPath := filepath.Join(p.config.KeyStorePath, ".master.key")
	if data, err := os.ReadFile(masterKeyPath); err == nil {
		p.masterKey = data
		return nil
	}

	// Generate new master key
	p.masterKey = make([]byte, 32)
	if _, err := rand.Read(p.masterKey); err != nil {
		return fmt.Errorf("failed to generate master key: %w", err)
	}

	// Save master key
	err := os.WriteFile(masterKeyPath, p.masterKey, 0600)
	if err != nil {
		return fmt.Errorf("failed to save master key: %w", err)
	}

	return nil
}

// loadKeys loads all existing keys from disk.
func (p *Provider) loadKeys() error {
	entries, err := os.ReadDir(p.config.KeyStorePath)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".key" {
			continue
		}

		keyPath := filepath.Join(p.config.KeyStorePath, entry.Name())

		data, err := os.ReadFile(keyPath)
		if err != nil {
			continue
		}

		var sk storedKey
		if err := json.Unmarshal(data, &sk); err != nil {
			continue
		}

		// Decrypt key material
		sk.PlainMaterial, err = p.decryptKeyMaterial(sk.Material)
		if err != nil {
			continue
		}

		p.keys[sk.Info.KeyID] = &sk
	}

	return nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "local"
}

// GenerateDataKey generates a new data encryption key.
func (p *Provider) GenerateDataKey(ctx context.Context, keyID string, keySpec kms.KeySpec) (*kms.DataKey, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	key, exists := p.keys[keyID]
	p.mu.RUnlock()

	if !exists {
		return nil, kms.ErrKeyNotFound
	}

	if key.Info.State != kms.KeyStateEnabled {
		return nil, kms.ErrKeyDisabled
	}

	// Determine key size based on spec
	keySize := 32 // Default AES-256

	algorithm := kms.AlgorithmAES256GCM
	switch keySpec.Algorithm {
	case kms.AlgorithmAES128:
		keySize = 16
		algorithm = kms.AlgorithmAES128
	case kms.AlgorithmAES256, kms.AlgorithmAES256GCM:
		keySize = 32
		algorithm = kms.AlgorithmAES256GCM
	case kms.AlgorithmChaCha20:
		keySize = 32
		algorithm = kms.AlgorithmChaCha20
	}

	// Generate random data key
	plaintext := make([]byte, keySize)
	if _, err := rand.Read(plaintext); err != nil {
		return nil, &kms.WrapError{Op: "GenerateDataKey", KeyID: keyID, Err: err}
	}

	// Encrypt the data key with the master key (for this key)
	ciphertext, err := p.encryptWithKey(key.PlainMaterial, plaintext)
	if err != nil {
		return nil, &kms.WrapError{Op: "GenerateDataKey", KeyID: keyID, Err: err}
	}

	return &kms.DataKey{
		KeyID:      keyID,
		Plaintext:  plaintext,
		Ciphertext: ciphertext,
		Algorithm:  algorithm,
	}, nil
}

// DecryptDataKey decrypts a previously encrypted data key.
func (p *Provider) DecryptDataKey(ctx context.Context, keyID string, encryptedKey []byte) ([]byte, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	key, exists := p.keys[keyID]
	p.mu.RUnlock()

	if !exists {
		return nil, kms.ErrKeyNotFound
	}

	if key.Info.State != kms.KeyStateEnabled {
		return nil, kms.ErrKeyDisabled
	}

	plaintext, err := p.decryptWithKey(key.PlainMaterial, encryptedKey)
	if err != nil {
		return nil, &kms.WrapError{Op: "DecryptDataKey", KeyID: keyID, Err: kms.ErrDecryptionFailed}
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

	key, exists := p.keys[keyID]
	p.mu.RUnlock()

	if !exists {
		return nil, kms.ErrKeyNotFound
	}

	if key.Info.State != kms.KeyStateEnabled {
		return nil, kms.ErrKeyDisabled
	}

	ciphertext, err := p.encryptWithKey(key.PlainMaterial, plaintext)
	if err != nil {
		return nil, &kms.WrapError{Op: "Encrypt", KeyID: keyID, Err: kms.ErrEncryptionFailed}
	}

	return ciphertext, nil
}

// Decrypt decrypts data using the specified key.
func (p *Provider) Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error) {
	p.mu.RLock()

	if p.closed {
		p.mu.RUnlock()
		return nil, kms.ErrProviderClosed
	}

	key, exists := p.keys[keyID]
	p.mu.RUnlock()

	if !exists {
		return nil, kms.ErrKeyNotFound
	}

	if key.Info.State != kms.KeyStateEnabled {
		return nil, kms.ErrKeyDisabled
	}

	plaintext, err := p.decryptWithKey(key.PlainMaterial, ciphertext)
	if err != nil {
		return nil, &kms.WrapError{Op: "Decrypt", KeyID: keyID, Err: kms.ErrDecryptionFailed}
	}

	return plaintext, nil
}

// ListKeys returns available encryption keys.
func (p *Provider) ListKeys(ctx context.Context) ([]kms.KeyInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, kms.ErrProviderClosed
	}

	keys := make([]kms.KeyInfo, 0, len(p.keys))
	for _, key := range p.keys {
		keys = append(keys, key.Info)
	}

	return keys, nil
}

// GetKeyInfo returns information about a specific key.
func (p *Provider) GetKeyInfo(ctx context.Context, keyID string) (*kms.KeyInfo, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, kms.ErrProviderClosed
	}

	key, exists := p.keys[keyID]
	if !exists {
		return nil, kms.ErrKeyNotFound
	}

	info := key.Info

	return &info, nil
}

// CreateKey creates a new encryption key.
func (p *Provider) CreateKey(ctx context.Context, spec kms.KeySpec) (*kms.KeyInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, kms.ErrProviderClosed
	}

	// Generate key ID
	keyIDBytes := make([]byte, 16)
	if _, err := rand.Read(keyIDBytes); err != nil {
		return nil, &kms.WrapError{Op: "CreateKey", Err: err}
	}

	keyID := hex.EncodeToString(keyIDBytes)

	// Determine key size
	keySize := 32

	switch spec.Algorithm {
	case kms.AlgorithmAES128:
		keySize = 16
	case kms.AlgorithmAES256, kms.AlgorithmAES256GCM, kms.AlgorithmChaCha20:
		keySize = 32
	case kms.AlgorithmRSA2048, kms.AlgorithmRSA3072, kms.AlgorithmRSA4096:
		return nil, &kms.WrapError{Op: "CreateKey", Err: errors.New("RSA keys not supported in local provider")}
	case kms.AlgorithmECDSAP256, kms.AlgorithmECDSAP384:
		return nil, &kms.WrapError{Op: "CreateKey", Err: errors.New("ECDSA keys not supported in local provider")}
	}

	// Generate key material
	keyMaterial := make([]byte, keySize)
	if _, err := rand.Read(keyMaterial); err != nil {
		return nil, &kms.WrapError{Op: "CreateKey", Err: err}
	}

	// Encrypt key material with master key
	encryptedMaterial, err := p.encryptKeyMaterial(keyMaterial)
	if err != nil {
		return nil, &kms.WrapError{Op: "CreateKey", Err: err}
	}

	now := time.Now()
	info := kms.KeyInfo{
		KeyID:       keyID,
		Name:        spec.Name,
		Description: spec.Description,
		Algorithm:   spec.Algorithm,
		Usage:       spec.Usage,
		State:       kms.KeyStateEnabled,
		CreatedAt:   now,
		Metadata:    spec.Metadata,
	}

	sk := &storedKey{
		Info:          info,
		Material:      encryptedMaterial,
		PlainMaterial: keyMaterial,
	}

	// Save to disk
	if err := p.saveKey(sk); err != nil {
		return nil, &kms.WrapError{Op: "CreateKey", Err: err}
	}

	p.keys[keyID] = sk

	return &info, nil
}

// RotateKey rotates an encryption key.
func (p *Provider) RotateKey(ctx context.Context, keyID string) (*kms.KeyInfo, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, kms.ErrProviderClosed
	}

	key, exists := p.keys[keyID]
	if !exists {
		return nil, kms.ErrKeyNotFound
	}

	if key.Info.State != kms.KeyStateEnabled {
		return nil, kms.ErrKeyDisabled
	}

	// Generate new key material
	keyMaterial := make([]byte, len(key.PlainMaterial))
	if _, err := rand.Read(keyMaterial); err != nil {
		return nil, &kms.WrapError{Op: "RotateKey", KeyID: keyID, Err: err}
	}

	// Encrypt new key material
	encryptedMaterial, err := p.encryptKeyMaterial(keyMaterial)
	if err != nil {
		return nil, &kms.WrapError{Op: "RotateKey", KeyID: keyID, Err: err}
	}

	// Update key
	now := time.Now()
	key.Material = encryptedMaterial
	key.PlainMaterial = keyMaterial
	key.Info.RotatedAt = &now

	// Save to disk
	if err := p.saveKey(key); err != nil {
		return nil, &kms.WrapError{Op: "RotateKey", KeyID: keyID, Err: err}
	}

	info := key.Info

	return &info, nil
}

// DeleteKey schedules a key for deletion.
func (p *Provider) DeleteKey(ctx context.Context, keyID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return kms.ErrProviderClosed
	}

	key, exists := p.keys[keyID]
	if !exists {
		return kms.ErrKeyNotFound
	}

	// Mark as pending deletion (30 day grace period for real production)
	// For local provider, we just mark it and can delete immediately
	deletionDate := time.Now().Add(24 * time.Hour)
	key.Info.State = kms.KeyStatePendingDeletion
	key.Info.DeletionDate = &deletionDate

	// Save updated state
	err := p.saveKey(key)
	if err != nil {
		return &kms.WrapError{Op: "DeleteKey", KeyID: keyID, Err: err}
	}

	return nil
}

// Close closes the provider.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true

	// Clear sensitive data from memory
	for i := range p.masterKey {
		p.masterKey[i] = 0
	}

	for _, key := range p.keys {
		for i := range key.PlainMaterial {
			key.PlainMaterial[i] = 0
		}
	}

	return nil
}

// saveKey saves a key to disk.
func (p *Provider) saveKey(sk *storedKey) error {
	data, err := json.MarshalIndent(sk, "", "  ")
	if err != nil {
		return err
	}

	keyPath := filepath.Join(p.config.KeyStorePath, sk.Info.KeyID+".key")

	return os.WriteFile(keyPath, data, 0600)
}

// encryptKeyMaterial encrypts key material with the master key.
func (p *Provider) encryptKeyMaterial(plaintext []byte) ([]byte, error) {
	return p.encryptWithKey(p.masterKey, plaintext)
}

// decryptKeyMaterial decrypts key material with the master key.
func (p *Provider) decryptKeyMaterial(ciphertext []byte) ([]byte, error) {
	return p.decryptWithKey(p.masterKey, ciphertext)
}

// encryptWithKey encrypts data with the given key using AES-GCM.
func (p *Provider) encryptWithKey(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptWithKey decrypts data with the given key using AES-GCM.
func (p *Provider) decryptWithKey(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:gcm.NonceSize()]
	ciphertext = ciphertext[gcm.NonceSize():]

	return gcm.Open(nil, nonce, ciphertext, nil)
}

// Ensure Provider implements kms.Provider.
var _ kms.Provider = (*Provider)(nil)
