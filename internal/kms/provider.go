// Package kms provides key management services for encryption at rest.
//
// The package defines a Provider interface that abstracts key management
// operations, with implementations for:
//   - Local KMS: File-based key storage for development
//   - HashiCorp Vault: Enterprise secret management
//   - AWS KMS: Amazon's managed key service
//   - Azure Key Vault: Azure's key management
//   - GCP Cloud KMS: Google's key management
//
// Features include:
//   - Data key generation and encryption
//   - Automatic key rotation
//   - Key caching for performance
//   - Audit logging of key operations
//
// Example usage:
//
//	provider, err := kms.NewVaultProvider(config)
//	plaintext, encrypted, err := provider.GenerateDataKey(ctx, keyID)
package kms

import (
	"context"
	"errors"
	"time"
)

// Provider is the interface for Key Management Services
type Provider interface {
	// Name returns the provider name
	Name() string

	// GenerateDataKey generates a new data encryption key
	// Returns plaintext key for immediate use and encrypted key for storage
	GenerateDataKey(ctx context.Context, keyID string, keySpec KeySpec) (*DataKey, error)

	// DecryptDataKey decrypts a previously encrypted data key
	DecryptDataKey(ctx context.Context, keyID string, encryptedKey []byte) ([]byte, error)

	// Encrypt encrypts data using the specified key
	Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error)

	// Decrypt decrypts data using the specified key
	Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error)

	// ListKeys returns available encryption keys
	ListKeys(ctx context.Context) ([]KeyInfo, error)

	// GetKeyInfo returns information about a specific key
	GetKeyInfo(ctx context.Context, keyID string) (*KeyInfo, error)

	// CreateKey creates a new encryption key
	CreateKey(ctx context.Context, spec KeySpec) (*KeyInfo, error)

	// RotateKey rotates an encryption key
	RotateKey(ctx context.Context, keyID string) (*KeyInfo, error)

	// DeleteKey schedules a key for deletion
	DeleteKey(ctx context.Context, keyID string) error

	// Close closes the provider connection
	Close() error
}

// KeySpec specifies key creation parameters
type KeySpec struct {
	// Name is the key name/alias
	Name string `json:"name"`
	// Description of the key
	Description string `json:"description,omitempty"`
	// Algorithm is the encryption algorithm (e.g., AES_256, RSA_4096)
	Algorithm Algorithm `json:"algorithm"`
	// Usage specifies how the key can be used
	Usage KeyUsage `json:"usage"`
	// Metadata is custom key metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Algorithm represents encryption algorithms
type Algorithm string

const (
	// Symmetric encryption algorithms
	AlgorithmAES128      Algorithm = "AES_128"
	AlgorithmAES256      Algorithm = "AES_256"
	AlgorithmAES256GCM   Algorithm = "AES_256_GCM"
	AlgorithmChaCha20    Algorithm = "CHACHA20_POLY1305"

	// Asymmetric encryption algorithms
	AlgorithmRSA2048     Algorithm = "RSA_2048"
	AlgorithmRSA3072     Algorithm = "RSA_3072"
	AlgorithmRSA4096     Algorithm = "RSA_4096"
	AlgorithmECDSAP256   Algorithm = "ECDSA_P256"
	AlgorithmECDSAP384   Algorithm = "ECDSA_P384"
)

// KeyUsage specifies how a key can be used
type KeyUsage string

const (
	// KeyUsageEncrypt means key is for encryption/decryption
	KeyUsageEncrypt KeyUsage = "ENCRYPT_DECRYPT"
	// KeyUsageSign means key is for signing/verification
	KeyUsageSign KeyUsage = "SIGN_VERIFY"
	// KeyUsageWrap means key is for wrapping other keys
	KeyUsageWrap KeyUsage = "KEY_WRAP"
)

// KeyState represents the state of a key
type KeyState string

const (
	KeyStateEnabled         KeyState = "ENABLED"
	KeyStateDisabled        KeyState = "DISABLED"
	KeyStatePendingDeletion KeyState = "PENDING_DELETION"
	KeyStateDeleted         KeyState = "DELETED"
)

// KeyInfo contains information about an encryption key
type KeyInfo struct {
	// KeyID is the unique key identifier
	KeyID string `json:"keyId"`
	// Name is the key name/alias
	Name string `json:"name,omitempty"`
	// Description of the key
	Description string `json:"description,omitempty"`
	// Algorithm used by this key
	Algorithm Algorithm `json:"algorithm"`
	// Usage specifies how the key can be used
	Usage KeyUsage `json:"usage"`
	// State is the current key state
	State KeyState `json:"state"`
	// CreatedAt is when the key was created
	CreatedAt time.Time `json:"createdAt"`
	// RotatedAt is when the key was last rotated
	RotatedAt *time.Time `json:"rotatedAt,omitempty"`
	// DeletionDate is when the key will be deleted (if pending)
	DeletionDate *time.Time `json:"deletionDate,omitempty"`
	// Metadata is custom key metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// DataKey contains a data encryption key
type DataKey struct {
	// KeyID is the ID of the master key used to encrypt this data key
	KeyID string `json:"keyId"`
	// Plaintext is the unencrypted data key (for immediate use)
	Plaintext []byte `json:"-"`
	// Ciphertext is the encrypted data key (for storage)
	Ciphertext []byte `json:"ciphertext"`
	// Algorithm used for the data key
	Algorithm Algorithm `json:"algorithm"`
}

// ProviderConfig is the base configuration for KMS providers
type ProviderConfig struct {
	// Type is the provider type (vault, aws, gcp, azure, local)
	Type string `json:"type" yaml:"type"`
	// Enabled indicates if this provider is active
	Enabled bool `json:"enabled" yaml:"enabled"`
	// DefaultKeyID is the default key for encryption
	DefaultKeyID string `json:"defaultKeyId,omitempty" yaml:"defaultKeyId,omitempty"`
	// KeyCacheTTL is how long to cache decrypted data keys
	KeyCacheTTL time.Duration `json:"keyCacheTtl,omitempty" yaml:"keyCacheTtl,omitempty"`
}

// Common errors
var (
	ErrKeyNotFound       = errors.New("key not found")
	ErrKeyDisabled       = errors.New("key is disabled")
	ErrInvalidKeyID      = errors.New("invalid key ID")
	ErrDecryptionFailed  = errors.New("decryption failed")
	ErrEncryptionFailed  = errors.New("encryption failed")
	ErrProviderClosed    = errors.New("provider is closed")
	ErrUnsupportedOp     = errors.New("operation not supported")
	ErrKeyRotationFailed = errors.New("key rotation failed")
)

// WrapError wraps an error with additional context
type WrapError struct {
	Op      string
	KeyID   string
	Err     error
}

func (e *WrapError) Error() string {
	if e.KeyID != "" {
		return e.Op + " [" + e.KeyID + "]: " + e.Err.Error()
	}
	return e.Op + ": " + e.Err.Error()
}

func (e *WrapError) Unwrap() error {
	return e.Err
}
