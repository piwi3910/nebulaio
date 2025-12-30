// Package encryption provides encryption key rotation automation for NebulaIO
package encryption

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"golang.org/x/crypto/hkdf"
)

// KeyType represents the type of encryption key.
type KeyType string

const (
	KeyTypeMaster   KeyType = "MASTER"
	KeyTypeData     KeyType = "DATA"
	KeyTypeBucket   KeyType = "BUCKET"
	KeyTypeObject   KeyType = "OBJECT"
	KeyTypeCustomer KeyType = "CUSTOMER" // Customer-managed key
)

// KeyStatus represents the status of a key.
type KeyStatus string

const (
	KeyStatusActive          KeyStatus = "ACTIVE"
	KeyStatusRotating        KeyStatus = "ROTATING"
	KeyStatusPendingDeletion KeyStatus = "PENDING_DELETION"
	KeyStatusDisabled        KeyStatus = "DISABLED"
	KeyStatusDeleted         KeyStatus = "DELETED"
)

// KeyAlgorithm represents the encryption algorithm.
type KeyAlgorithm string

const (
	KeyAlgorithmAES256GCM KeyAlgorithm = "AES-256-GCM"
	KeyAlgorithmAES256CBC KeyAlgorithm = "AES-256-CBC"
	KeyAlgorithmChaCha20  KeyAlgorithm = "CHACHA20-POLY1305"
)

// EncryptionKey represents an encryption key.
type EncryptionKey struct {
	CreatedAt   time.Time         `json:"created_at"`
	LastUsedAt  *time.Time        `json:"last_used_at,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	DeleteAfter *time.Time        `json:"delete_after,omitempty"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
	RotatedAt   *time.Time        `json:"rotated_at,omitempty"`
	Status      KeyStatus         `json:"status"`
	ID          string            `json:"id"`
	Algorithm   KeyAlgorithm      `json:"algorithm"`
	ParentKeyID string            `json:"parent_key_id,omitempty"`
	Type        KeyType           `json:"type"`
	Alias       string            `json:"alias,omitempty"`
	WrappedKey  []byte            `json:"wrapped_key,omitempty"`
	KeyMaterial []byte            `json:"-"`
	Version     int               `json:"version"`
	UsageCount  int64             `json:"usage_count"`
}

// KeyVersion represents a version of a key.
type KeyVersion struct {
	CreatedAt   time.Time `json:"created_at"`
	Status      KeyStatus `json:"status"`
	KeyMaterial []byte    `json:"-"`
	WrappedKey  []byte    `json:"wrapped_key"`
	Version     int       `json:"version"`
	RotatedFrom int       `json:"rotated_from,omitempty"`
}

// RotationPolicy defines when and how keys should be rotated.
type RotationPolicy struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	KeyType             KeyType       `json:"key_type"`
	RotationInterval    time.Duration `json:"rotation_interval"`
	MaxKeyAge           time.Duration `json:"max_key_age"`
	GracePeriod         time.Duration `json:"grace_period"`
	NotifyBeforeExpiry  time.Duration `json:"notify_before_expiry"`
	RetainVersions      int           `json:"retain_versions"`
	AutoRotate          bool          `json:"auto_rotate"`
	RequireReencryption bool          `json:"require_reencryption"`
	Enabled             bool          `json:"enabled"`
}

// RotationEvent represents a key rotation event.
type RotationEvent struct {
	Timestamp          time.Time     `json:"timestamp"`
	ID                 string        `json:"id"`
	KeyID              string        `json:"key_id"`
	Trigger            string        `json:"trigger"`
	Status             string        `json:"status"`
	Error              string        `json:"error,omitempty"`
	InitiatedBy        string        `json:"initiated_by"`
	OldVersion         int           `json:"old_version"`
	NewVersion         int           `json:"new_version"`
	ObjectsReencrypted int64         `json:"objects_reencrypted"`
	Duration           time.Duration `json:"duration"`
}

// ReencryptionJob represents a job to re-encrypt objects with a new key.
type ReencryptionJob struct {
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	KeyID            string     `json:"key_id"`
	ID               string     `json:"id"`
	Status           string     `json:"status"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	Buckets          []string   `json:"buckets,omitempty"`
	NewVersion       int        `json:"new_version"`
	FailedObjects    int64      `json:"failed_objects"`
	ProcessedObjects int64      `json:"processed_objects"`
	TotalObjects     int64      `json:"total_objects"`
	OldVersion       int        `json:"old_version"`
	Concurrency      int        `json:"concurrency"`
}

// KeyRotationConfig contains configuration for the key rotation manager.
type KeyRotationConfig struct {
	DefaultRotationInterval time.Duration `json:"default_rotation_interval"`
	DefaultGracePeriod      time.Duration `json:"default_grace_period"`
	DefaultRetainVersions   int           `json:"default_retain_versions"`
	ReencryptionConcurrency int           `json:"reencryption_concurrency"`
	CheckInterval           time.Duration `json:"check_interval"`
	EnableAutoRotation      bool          `json:"enable_auto_rotation"`
}

// KeyRotationManager manages encryption key rotation.
type KeyRotationManager struct {
	storage        KeyStorage
	notifier       KeyRotationNotifier
	config         *KeyRotationConfig
	keys           map[string]*EncryptionKey
	keyVersions    map[string][]*KeyVersion
	policies       map[string]*RotationPolicy
	reencryptJobs  map[string]*ReencryptionJob
	stopChan       chan struct{}
	rotationEvents []*RotationEvent
	masterKey      []byte
	wg             sync.WaitGroup
	mu             sync.RWMutex
}

// KeyStorage defines the interface for key persistence.
type KeyStorage interface {
	StoreKey(ctx context.Context, key *EncryptionKey) error
	GetKey(ctx context.Context, keyID string) (*EncryptionKey, error)
	ListKeys(ctx context.Context, keyType KeyType) ([]*EncryptionKey, error)
	DeleteKey(ctx context.Context, keyID string) error
	StoreKeyVersion(ctx context.Context, keyID string, version *KeyVersion) error
	GetKeyVersions(ctx context.Context, keyID string) ([]*KeyVersion, error)
	StoreRotationEvent(ctx context.Context, event *RotationEvent) error
	GetRotationEvents(ctx context.Context, keyID string) ([]*RotationEvent, error)
	StoreReencryptionJob(ctx context.Context, job *ReencryptionJob) error
	GetReencryptionJob(ctx context.Context, jobID string) (*ReencryptionJob, error)
}

// KeyRotationNotifier defines the interface for rotation notifications.
type KeyRotationNotifier interface {
	NotifyRotationStarted(ctx context.Context, event *RotationEvent) error
	NotifyRotationCompleted(ctx context.Context, event *RotationEvent) error
	NotifyRotationFailed(ctx context.Context, event *RotationEvent) error
	NotifyKeyExpiring(ctx context.Context, key *EncryptionKey, expiresIn time.Duration) error
}

// ObjectReencryptor defines the interface for re-encrypting objects.
type ObjectReencryptor interface {
	ListObjectsForKey(ctx context.Context, keyID string, version int) ([]string, error)
	ReencryptObject(ctx context.Context, objectRef string, oldKey, newKey *EncryptionKey) error
}

// NewKeyRotationManager creates a new key rotation manager.
func NewKeyRotationManager(config *KeyRotationConfig, storage KeyStorage, notifier KeyRotationNotifier, masterKey []byte) *KeyRotationManager {
	if config == nil {
		config = &KeyRotationConfig{
			DefaultRotationInterval: 90 * 24 * time.Hour, // 90 days
			DefaultGracePeriod:      7 * 24 * time.Hour,  // 7 days
			DefaultRetainVersions:   5,
			ReencryptionConcurrency: 10,
			CheckInterval:           time.Hour,
			EnableAutoRotation:      true,
		}
	}

	krm := &KeyRotationManager{
		config:         config,
		keys:           make(map[string]*EncryptionKey),
		keyVersions:    make(map[string][]*KeyVersion),
		policies:       make(map[string]*RotationPolicy),
		rotationEvents: make([]*RotationEvent, 0),
		reencryptJobs:  make(map[string]*ReencryptionJob),
		storage:        storage,
		notifier:       notifier,
		masterKey:      masterKey,
		stopChan:       make(chan struct{}),
	}

	// Initialize default policies
	krm.initializeDefaultPolicies()

	// Start background rotation checker
	if config.EnableAutoRotation {
		krm.wg.Add(1)

		go krm.rotationChecker()
	}

	return krm
}

// initializeDefaultPolicies sets up default rotation policies.
func (krm *KeyRotationManager) initializeDefaultPolicies() {
	defaultPolicies := []*RotationPolicy{
		{
			ID:                  "master-key-policy",
			Name:                "Master Key Rotation Policy",
			KeyType:             KeyTypeMaster,
			RotationInterval:    365 * 24 * time.Hour, // 1 year
			MaxKeyAge:           400 * 24 * time.Hour,
			GracePeriod:         30 * 24 * time.Hour,
			AutoRotate:          true,
			NotifyBeforeExpiry:  30 * 24 * time.Hour,
			RetainVersions:      3,
			RequireReencryption: true,
			Enabled:             true,
		},
		{
			ID:                  "data-key-policy",
			Name:                "Data Key Rotation Policy",
			KeyType:             KeyTypeData,
			RotationInterval:    90 * 24 * time.Hour, // 90 days
			MaxKeyAge:           120 * 24 * time.Hour,
			GracePeriod:         7 * 24 * time.Hour,
			AutoRotate:          true,
			NotifyBeforeExpiry:  14 * 24 * time.Hour,
			RetainVersions:      5,
			RequireReencryption: true,
			Enabled:             true,
		},
		{
			ID:                  "bucket-key-policy",
			Name:                "Bucket Key Rotation Policy",
			KeyType:             KeyTypeBucket,
			RotationInterval:    180 * 24 * time.Hour, // 6 months
			MaxKeyAge:           210 * 24 * time.Hour,
			GracePeriod:         14 * 24 * time.Hour,
			AutoRotate:          true,
			NotifyBeforeExpiry:  21 * 24 * time.Hour,
			RetainVersions:      3,
			RequireReencryption: false,
			Enabled:             true,
		},
	}

	for _, policy := range defaultPolicies {
		krm.policies[policy.ID] = policy
	}
}

// CreateKey creates a new encryption key.
func (krm *KeyRotationManager) CreateKey(ctx context.Context, keyType KeyType, algorithm KeyAlgorithm, alias string, metadata map[string]string) (*EncryptionKey, error) {
	keyMaterial, err := krm.generateKeyMaterial(algorithm)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key material: %w", err)
	}

	wrappedKey, err := krm.wrapKey(keyMaterial)
	if err != nil {
		return nil, fmt.Errorf("failed to wrap key: %w", err)
	}

	now := time.Now()
	key := &EncryptionKey{
		ID:          uuid.New().String(),
		Alias:       alias,
		Type:        keyType,
		Algorithm:   algorithm,
		Status:      KeyStatusActive,
		Version:     1,
		KeyMaterial: keyMaterial,
		WrappedKey:  wrappedKey,
		CreatedAt:   now,
		Metadata:    metadata,
	}

	// Set expiration based on policy
	if policy := krm.getPolicyForKeyType(keyType); policy != nil {
		expiresAt := now.Add(policy.MaxKeyAge)
		key.ExpiresAt = &expiresAt
	}

	krm.mu.Lock()
	krm.keys[key.ID] = key
	krm.keyVersions[key.ID] = []*KeyVersion{
		{
			Version:     1,
			KeyMaterial: keyMaterial,
			WrappedKey:  wrappedKey,
			CreatedAt:   now,
			Status:      KeyStatusActive,
		},
	}
	krm.mu.Unlock()

	if krm.storage != nil {
		err := krm.storage.StoreKey(ctx, key)
		if err != nil {
			return nil, err
		}
	}

	return key, nil
}

// generateKeyMaterial generates new key material based on algorithm.
func (krm *KeyRotationManager) generateKeyMaterial(algorithm KeyAlgorithm) ([]byte, error) {
	var keySize int

	switch algorithm {
	case KeyAlgorithmAES256GCM, KeyAlgorithmAES256CBC:
		keySize = 32 // 256 bits
	case KeyAlgorithmChaCha20:
		keySize = 32 // 256 bits
	default:
		keySize = 32
	}

	key := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}

	return key, nil
}

// wrapKey wraps a key with the master key.
func (krm *KeyRotationManager) wrapKey(keyMaterial []byte) ([]byte, error) {
	if len(krm.masterKey) == 0 {
		return nil, errors.New("master key not set")
	}

	block, err := aes.NewCipher(krm.masterKey)
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

	return gcm.Seal(nonce, nonce, keyMaterial, nil), nil
}

// unwrapKey unwraps a key using the master key.
func (krm *KeyRotationManager) unwrapKey(wrappedKey []byte) ([]byte, error) {
	if len(krm.masterKey) == 0 {
		return nil, errors.New("master key not set")
	}

	block, err := aes.NewCipher(krm.masterKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(wrappedKey) < nonceSize {
		return nil, errors.New("wrapped key too short")
	}

	nonce, ciphertext := wrappedKey[:nonceSize], wrappedKey[nonceSize:]

	return gcm.Open(nil, nonce, ciphertext, nil)
}

// RotateKey rotates an encryption key.
func (krm *KeyRotationManager) RotateKey(ctx context.Context, keyID string, trigger string, initiatedBy string) (*RotationEvent, error) {
	krm.mu.Lock()

	key, exists := krm.keys[keyID]
	if !exists {
		krm.mu.Unlock()
		return nil, fmt.Errorf("key not found: %s", keyID)
	}

	if key.Status != KeyStatusActive {
		krm.mu.Unlock()
		return nil, fmt.Errorf("key is not active: %s", key.Status)
	}

	oldVersion := key.Version
	key.Status = KeyStatusRotating

	krm.mu.Unlock()

	// Create rotation event
	event := &RotationEvent{
		ID:          uuid.New().String(),
		KeyID:       keyID,
		OldVersion:  oldVersion,
		NewVersion:  oldVersion + 1,
		Timestamp:   time.Now(),
		Trigger:     trigger,
		Status:      "started",
		InitiatedBy: initiatedBy,
	}

	// Notify rotation started
	if krm.notifier != nil {
		_ = krm.notifier.NotifyRotationStarted(ctx, event)
	}

	// Generate new key material
	newKeyMaterial, err := krm.generateKeyMaterial(key.Algorithm)
	if err != nil {
		event.Status = "failed"
		event.Error = err.Error()
		krm.storeRotationEvent(ctx, event)

		if krm.notifier != nil {
			_ = krm.notifier.NotifyRotationFailed(ctx, event)
		}

		return event, err
	}

	wrappedKey, err := krm.wrapKey(newKeyMaterial)
	if err != nil {
		event.Status = "failed"
		event.Error = err.Error()
		krm.storeRotationEvent(ctx, event)

		if krm.notifier != nil {
			_ = krm.notifier.NotifyRotationFailed(ctx, event)
		}

		return event, err
	}

	// Update key
	krm.mu.Lock()

	now := time.Now()
	key.Version = oldVersion + 1
	key.KeyMaterial = newKeyMaterial
	key.WrappedKey = wrappedKey
	key.RotatedAt = &now
	key.Status = KeyStatusActive

	// Update expiration
	if policy := krm.getPolicyForKeyType(key.Type); policy != nil {
		expiresAt := now.Add(policy.MaxKeyAge)
		key.ExpiresAt = &expiresAt
	}

	// Store new version
	newVersion := &KeyVersion{
		Version:     key.Version,
		KeyMaterial: newKeyMaterial,
		WrappedKey:  wrappedKey,
		CreatedAt:   now,
		Status:      KeyStatusActive,
		RotatedFrom: oldVersion,
	}
	krm.keyVersions[keyID] = append(krm.keyVersions[keyID], newVersion)

	// Mark old version as disabled
	for _, v := range krm.keyVersions[keyID] {
		if v.Version == oldVersion {
			v.Status = KeyStatusDisabled
		}
	}

	krm.mu.Unlock()

	// Store updated key
	if krm.storage != nil {
		err := krm.storage.StoreKey(ctx, key)
		if err != nil {
			event.Status = "failed"
			event.Error = err.Error()

			return event, err
		}
		err = krm.storage.StoreKeyVersion(ctx, keyID, newVersion)

		if err != nil {
			event.Status = "failed"
			event.Error = err.Error()

			return event, err
		}
	}

	// Complete rotation event
	event.Status = "completed"
	event.Duration = time.Since(event.Timestamp)
	krm.storeRotationEvent(ctx, event)

	// Notify rotation completed
	if krm.notifier != nil {
		_ = krm.notifier.NotifyRotationCompleted(ctx, event)
	}

	return event, nil
}

// storeRotationEvent stores a rotation event.
func (krm *KeyRotationManager) storeRotationEvent(ctx context.Context, event *RotationEvent) {
	krm.mu.Lock()
	krm.rotationEvents = append(krm.rotationEvents, event)
	krm.mu.Unlock()

	if krm.storage != nil {
		_ = krm.storage.StoreRotationEvent(ctx, event)
	}
}

// getPolicyForKeyType returns the rotation policy for a key type.
func (krm *KeyRotationManager) getPolicyForKeyType(keyType KeyType) *RotationPolicy {
	for _, policy := range krm.policies {
		if policy.KeyType == keyType && policy.Enabled {
			return policy
		}
	}

	return nil
}

// rotationChecker checks for keys that need rotation.
func (krm *KeyRotationManager) rotationChecker() {
	defer krm.wg.Done()

	ticker := time.NewTicker(krm.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-krm.stopChan:
			return
		case <-ticker.C:
			krm.checkAndRotateKeys(context.Background())
		}
	}
}

// checkAndRotateKeys checks all keys and rotates those that need it.
func (krm *KeyRotationManager) checkAndRotateKeys(ctx context.Context) {
	krm.mu.RLock()

	keys := make([]*EncryptionKey, 0, len(krm.keys))
	for _, key := range krm.keys {
		keys = append(keys, key)
	}

	krm.mu.RUnlock()

	now := time.Now()

	for _, key := range keys {
		if key.Status != KeyStatusActive {
			continue
		}

		policy := krm.getPolicyForKeyType(key.Type)
		if policy == nil || !policy.AutoRotate {
			continue
		}

		// Check if key needs rotation based on age
		keyAge := now.Sub(key.CreatedAt)
		if key.RotatedAt != nil {
			keyAge = now.Sub(*key.RotatedAt)
		}

		if keyAge >= policy.RotationInterval {
			if _, rotateErr := krm.RotateKey(ctx, key.ID, "scheduled", "system"); rotateErr != nil {
				log.Error().
					Err(rotateErr).
					Str("key_id", key.ID).
					Str("key_type", string(key.Type)).
					Dur("key_age", keyAge).
					Msg("failed to rotate key during scheduled rotation - key may exceed rotation policy")
			}

			continue
		}

		// Check if key is expiring soon and notify
		if key.ExpiresAt != nil {
			timeToExpiry := key.ExpiresAt.Sub(now)
			if timeToExpiry <= policy.NotifyBeforeExpiry && timeToExpiry > 0 {
				if krm.notifier != nil {
					notifyErr := krm.notifier.NotifyKeyExpiring(ctx, key, timeToExpiry)
					if notifyErr != nil {
						log.Warn().
							Err(notifyErr).
							Str("key_id", key.ID).
							Dur("time_to_expiry", timeToExpiry).
							Msg("failed to send key expiry notification")
					}
				}
			}
		}
	}
}

// GetKey retrieves a key by ID.
func (krm *KeyRotationManager) GetKey(ctx context.Context, keyID string) (*EncryptionKey, error) {
	krm.mu.RLock()
	key, exists := krm.keys[keyID]
	krm.mu.RUnlock()

	if exists {
		return key, nil
	}

	if krm.storage != nil {
		key, err := krm.storage.GetKey(ctx, keyID)
		if err != nil {
			return nil, err
		}

		if key != nil {
			// Unwrap key material
			if len(key.WrappedKey) > 0 {
				keyMaterial, err := krm.unwrapKey(key.WrappedKey)
				if err != nil {
					return nil, err
				}

				key.KeyMaterial = keyMaterial
			}

			krm.mu.Lock()
			krm.keys[keyID] = key
			krm.mu.Unlock()

			return key, nil
		}
	}

	return nil, fmt.Errorf("key not found: %s", keyID)
}

// GetKeyByAlias retrieves a key by alias.
func (krm *KeyRotationManager) GetKeyByAlias(ctx context.Context, alias string) (*EncryptionKey, error) {
	krm.mu.RLock()

	for _, key := range krm.keys {
		if key.Alias == alias && key.Status == KeyStatusActive {
			krm.mu.RUnlock()
			return key, nil
		}
	}

	krm.mu.RUnlock()

	return nil, fmt.Errorf("key not found with alias: %s", alias)
}

// ListKeys lists all keys of a given type.
func (krm *KeyRotationManager) ListKeys(ctx context.Context, keyType KeyType) ([]*EncryptionKey, error) {
	if krm.storage != nil {
		return krm.storage.ListKeys(ctx, keyType)
	}

	krm.mu.RLock()
	defer krm.mu.RUnlock()

	var keys []*EncryptionKey
	for _, key := range krm.keys {
		if keyType == "" || key.Type == keyType {
			// Don't include key material in list
			keyCopy := *key
			keyCopy.KeyMaterial = nil
			keys = append(keys, &keyCopy)
		}
	}

	return keys, nil
}

// DisableKey disables a key.
func (krm *KeyRotationManager) DisableKey(ctx context.Context, keyID string) error {
	krm.mu.Lock()

	key, exists := krm.keys[keyID]
	if !exists {
		krm.mu.Unlock()
		return fmt.Errorf("key not found: %s", keyID)
	}

	key.Status = KeyStatusDisabled

	krm.mu.Unlock()

	if krm.storage != nil {
		return krm.storage.StoreKey(ctx, key)
	}

	return nil
}

// ScheduleKeyDeletion schedules a key for deletion.
func (krm *KeyRotationManager) ScheduleKeyDeletion(ctx context.Context, keyID string, deleteAfter time.Duration) error {
	krm.mu.Lock()

	key, exists := krm.keys[keyID]
	if !exists {
		krm.mu.Unlock()
		return fmt.Errorf("key not found: %s", keyID)
	}

	deleteTime := time.Now().Add(deleteAfter)
	key.Status = KeyStatusPendingDeletion
	key.DeleteAfter = &deleteTime

	krm.mu.Unlock()

	if krm.storage != nil {
		return krm.storage.StoreKey(ctx, key)
	}

	return nil
}

// CancelKeyDeletion cancels a scheduled key deletion.
func (krm *KeyRotationManager) CancelKeyDeletion(ctx context.Context, keyID string) error {
	krm.mu.Lock()

	key, exists := krm.keys[keyID]
	if !exists {
		krm.mu.Unlock()
		return fmt.Errorf("key not found: %s", keyID)
	}

	if key.Status != KeyStatusPendingDeletion {
		krm.mu.Unlock()
		return errors.New("key is not pending deletion")
	}

	key.Status = KeyStatusDisabled
	key.DeleteAfter = nil

	krm.mu.Unlock()

	if krm.storage != nil {
		return krm.storage.StoreKey(ctx, key)
	}

	return nil
}

// GetKeyVersion retrieves a specific version of a key.
func (krm *KeyRotationManager) GetKeyVersion(ctx context.Context, keyID string, version int) (*KeyVersion, error) {
	krm.mu.RLock()
	versions, exists := krm.keyVersions[keyID]
	krm.mu.RUnlock()

	if exists {
		for _, v := range versions {
			if v.Version == version {
				return v, nil
			}
		}
	}

	if krm.storage != nil {
		versions, err := krm.storage.GetKeyVersions(ctx, keyID)
		if err != nil {
			return nil, err
		}

		for _, v := range versions {
			if v.Version == version {
				// Unwrap key material
				if len(v.WrappedKey) > 0 {
					keyMaterial, err := krm.unwrapKey(v.WrappedKey)
					if err != nil {
						return nil, err
					}

					v.KeyMaterial = keyMaterial
				}

				return v, nil
			}
		}
	}

	return nil, fmt.Errorf("key version not found: %s v%d", keyID, version)
}

// DeriveKey derives a new key from an existing key using HKDF.
func (krm *KeyRotationManager) DeriveKey(ctx context.Context, parentKeyID string, info []byte, keySize int) ([]byte, error) {
	key, err := krm.GetKey(ctx, parentKeyID)
	if err != nil {
		return nil, err
	}

	if len(key.KeyMaterial) == 0 {
		return nil, errors.New("parent key material not available")
	}

	// Use HKDF to derive a new key
	hkdfReader := hkdf.New(sha256.New, key.KeyMaterial, nil, info)

	derivedKey := make([]byte, keySize)
	if _, err := io.ReadFull(hkdfReader, derivedKey); err != nil {
		return nil, err
	}

	return derivedKey, nil
}

// Encrypt encrypts data using the specified key.
func (krm *KeyRotationManager) Encrypt(ctx context.Context, keyID string, plaintext []byte) ([]byte, error) {
	key, err := krm.GetKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	if key.Status != KeyStatusActive {
		return nil, fmt.Errorf("key is not active: %s", key.Status)
	}

	block, err := aes.NewCipher(key.KeyMaterial)
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

	// Update usage statistics
	krm.mu.Lock()

	key.UsageCount++
	now := time.Now()
	key.LastUsedAt = &now

	krm.mu.Unlock()

	// Prepend version number for decryption routing
	versionPrefix := []byte{byte(key.Version)}
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	return append(versionPrefix, ciphertext...), nil
}

// Decrypt decrypts data using the specified key.
func (krm *KeyRotationManager) Decrypt(ctx context.Context, keyID string, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < 2 {
		return nil, errors.New("ciphertext too short")
	}

	// Extract version from ciphertext
	version := int(ciphertext[0])
	ciphertext = ciphertext[1:]

	// Get the appropriate key version
	keyVersion, err := krm.GetKeyVersion(ctx, keyID, version)
	if err != nil {
		// Fallback to current key if version not found
		key, err := krm.GetKey(ctx, keyID)
		if err != nil {
			return nil, err
		}

		keyVersion = &KeyVersion{
			KeyMaterial: key.KeyMaterial,
		}
	}

	block, err := aes.NewCipher(keyVersion.KeyMaterial)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]

	return gcm.Open(nil, nonce, ciphertext, nil)
}

// StartReencryptionJob starts a job to re-encrypt objects with a new key version.
func (krm *KeyRotationManager) StartReencryptionJob(ctx context.Context, keyID string, oldVersion, newVersion int, buckets []string, reencryptor ObjectReencryptor) (*ReencryptionJob, error) {
	job := &ReencryptionJob{
		ID:          uuid.New().String(),
		KeyID:       keyID,
		OldVersion:  oldVersion,
		NewVersion:  newVersion,
		Status:      "pending",
		Buckets:     buckets,
		Concurrency: krm.config.ReencryptionConcurrency,
	}

	krm.mu.Lock()
	krm.reencryptJobs[job.ID] = job
	krm.mu.Unlock()

	// Start the job in background
	go krm.runReencryptionJob(ctx, job, reencryptor)

	return job, nil
}

// runReencryptionJob runs the re-encryption job.
func (krm *KeyRotationManager) runReencryptionJob(ctx context.Context, job *ReencryptionJob, reencryptor ObjectReencryptor) {
	now := time.Now()
	job.StartedAt = &now
	job.Status = "running"

	// Get old and new key versions
	oldKey, err := krm.GetKeyVersion(ctx, job.KeyID, job.OldVersion)
	if err != nil {
		job.Status = "failed"
		job.ErrorMessage = err.Error()

		return
	}

	newKeyData, err := krm.GetKey(ctx, job.KeyID)
	if err != nil {
		job.Status = "failed"
		job.ErrorMessage = err.Error()

		return
	}

	// Create temporary key structs for re-encryption
	oldKeyStruct := &EncryptionKey{
		ID:          job.KeyID,
		Version:     job.OldVersion,
		KeyMaterial: oldKey.KeyMaterial,
	}

	// List objects encrypted with old key version
	objects, err := reencryptor.ListObjectsForKey(ctx, job.KeyID, job.OldVersion)
	if err != nil {
		job.Status = "failed"
		job.ErrorMessage = err.Error()

		return
	}

	job.TotalObjects = int64(len(objects))

	// Process objects with concurrency limit
	semaphore := make(chan struct{}, job.Concurrency)

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	for _, objectRef := range objects {
		select {
		case <-ctx.Done():
			job.Status = "failed"
			job.ErrorMessage = ctx.Err().Error()

			return
		default:
		}

		wg.Add(1)

		semaphore <- struct{}{}

		go func(ref string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			err := reencryptor.ReencryptObject(ctx, ref, oldKeyStruct, newKeyData)
			if err != nil {
				mu.Lock()

				job.FailedObjects++

				mu.Unlock()
			} else {
				mu.Lock()

				job.ProcessedObjects++

				mu.Unlock()
			}
		}(objectRef)
	}

	wg.Wait()

	completedAt := time.Now()
	job.CompletedAt = &completedAt

	if job.FailedObjects > 0 {
		job.Status = "completed_with_errors"
	} else {
		job.Status = "completed"
	}

	if krm.storage != nil {
		_ = krm.storage.StoreReencryptionJob(ctx, job)
	}
}

// GetReencryptionJob retrieves a re-encryption job by ID.
func (krm *KeyRotationManager) GetReencryptionJob(ctx context.Context, jobID string) (*ReencryptionJob, error) {
	krm.mu.RLock()
	job, exists := krm.reencryptJobs[jobID]
	krm.mu.RUnlock()

	if exists {
		return job, nil
	}

	if krm.storage != nil {
		return krm.storage.GetReencryptionJob(ctx, jobID)
	}

	return nil, fmt.Errorf("job not found: %s", jobID)
}

// AddPolicy adds a custom rotation policy.
func (krm *KeyRotationManager) AddPolicy(policy *RotationPolicy) error {
	if policy.ID == "" {
		policy.ID = uuid.New().String()
	}

	krm.mu.Lock()
	krm.policies[policy.ID] = policy
	krm.mu.Unlock()

	return nil
}

// GetPolicies returns all rotation policies.
func (krm *KeyRotationManager) GetPolicies() []*RotationPolicy {
	krm.mu.RLock()
	defer krm.mu.RUnlock()

	policies := make([]*RotationPolicy, 0, len(krm.policies))
	for _, policy := range krm.policies {
		policies = append(policies, policy)
	}

	return policies
}

// GetRotationHistory returns rotation events for a key.
func (krm *KeyRotationManager) GetRotationHistory(ctx context.Context, keyID string) ([]*RotationEvent, error) {
	if krm.storage != nil {
		return krm.storage.GetRotationEvents(ctx, keyID)
	}

	krm.mu.RLock()
	defer krm.mu.RUnlock()

	var events []*RotationEvent

	for _, event := range krm.rotationEvents {
		if event.KeyID == keyID {
			events = append(events, event)
		}
	}

	return events, nil
}

// ExportKey exports a key in a safe format (wrapped).
func (krm *KeyRotationManager) ExportKey(ctx context.Context, keyID string) (string, error) {
	krm.mu.RLock()
	key, exists := krm.keys[keyID]
	krm.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("key not found: %s", keyID)
	}

	// Create export structure (without raw key material)
	export := map[string]interface{}{
		"id":          key.ID,
		"alias":       key.Alias,
		"type":        key.Type,
		"algorithm":   key.Algorithm,
		"version":     key.Version,
		"wrapped_key": base64.StdEncoding.EncodeToString(key.WrappedKey),
		"created_at":  key.CreatedAt,
		"metadata":    key.Metadata,
	}

	data, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// ImportKey imports a previously exported key.
func (krm *KeyRotationManager) ImportKey(ctx context.Context, exportedKey string) (*EncryptionKey, error) {
	var importData map[string]interface{}
	if err := json.Unmarshal([]byte(exportedKey), &importData); err != nil {
		return nil, err
	}

	wrappedKeyB64, ok := importData["wrapped_key"].(string)
	if !ok {
		return nil, errors.New("missing wrapped_key in import data")
	}

	wrappedKey, err := base64.StdEncoding.DecodeString(wrappedKeyB64)
	if err != nil {
		return nil, err
	}

	keyMaterial, err := krm.unwrapKey(wrappedKey)
	if err != nil {
		return nil, fmt.Errorf("failed to unwrap key: %w", err)
	}

	key := &EncryptionKey{
		ID:          importData["id"].(string),
		Alias:       importData["alias"].(string),
		Type:        KeyType(importData["type"].(string)),
		Algorithm:   KeyAlgorithm(importData["algorithm"].(string)),
		Status:      KeyStatusActive,
		Version:     int(importData["version"].(float64)),
		KeyMaterial: keyMaterial,
		WrappedKey:  wrappedKey,
		CreatedAt:   time.Now(),
	}

	if metadata, ok := importData["metadata"].(map[string]interface{}); ok {
		key.Metadata = make(map[string]string)
		for k, v := range metadata {
			if s, ok := v.(string); ok {
				key.Metadata[k] = s
			}
		}
	}

	krm.mu.Lock()
	krm.keys[key.ID] = key
	krm.mu.Unlock()

	if krm.storage != nil {
		err := krm.storage.StoreKey(ctx, key)
		if err != nil {
			return nil, err
		}
	}

	return key, nil
}

// Stop stops the key rotation manager.
func (krm *KeyRotationManager) Stop() {
	close(krm.stopChan)
	krm.wg.Wait()
}

// CleanupOldVersions removes old key versions based on retention policy.
func (krm *KeyRotationManager) CleanupOldVersions(ctx context.Context) error {
	krm.mu.Lock()
	defer krm.mu.Unlock()

	for keyID, versions := range krm.keyVersions {
		key, exists := krm.keys[keyID]
		if !exists {
			continue
		}

		policy := krm.getPolicyForKeyType(key.Type)
		if policy == nil {
			continue
		}

		// Keep only the most recent N versions
		if len(versions) > policy.RetainVersions {
			// Sort by version descending
			sortedVersions := make([]*KeyVersion, len(versions))
			copy(sortedVersions, versions)

			for i := range len(sortedVersions) - 1 {
				for j := i + 1; j < len(sortedVersions); j++ {
					if sortedVersions[i].Version < sortedVersions[j].Version {
						sortedVersions[i], sortedVersions[j] = sortedVersions[j], sortedVersions[i]
					}
				}
			}

			// Keep only RetainVersions
			krm.keyVersions[keyID] = sortedVersions[:policy.RetainVersions]
		}
	}

	return nil
}
