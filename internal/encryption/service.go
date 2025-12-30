package encryption

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/piwi3910/nebulaio/internal/kms"
	"golang.org/x/crypto/chacha20poly1305"
)

// Service configuration constants.
const (
	defaultKeyCacheTTL     = 5 * time.Minute // Default TTL for cached keys
	defaultKeyCacheMaxSize = 1000            // Default maximum number of cached keys
)

// Config holds encryption service configuration.
type Config struct {
	// DefaultKeyID is the default KMS key to use for encryption
	DefaultKeyID string `json:"defaultKeyId" yaml:"defaultKeyId"`

	// Algorithm is the encryption algorithm for data encryption
	Algorithm kms.Algorithm `json:"algorithm" yaml:"algorithm"`

	// KeyCacheTTL is how long to cache decrypted data keys
	KeyCacheTTL time.Duration `json:"keyCacheTtl" yaml:"keyCacheTtl"`

	// KeyCacheMaxSize is the maximum number of cached keys
	KeyCacheMaxSize int `json:"keyCacheMaxSize" yaml:"keyCacheMaxSize"`
}

// DefaultConfig returns a default encryption service configuration.
func DefaultConfig() Config {
	return Config{
		Algorithm:       kms.AlgorithmAES256GCM,
		KeyCacheTTL:     defaultKeyCacheTTL,
		KeyCacheMaxSize: defaultKeyCacheMaxSize,
	}
}

// Service provides envelope encryption using a KMS provider.
type Service struct {
	provider kms.Provider
	cache    *keyCache
	config   Config
}

// keyCache holds cached decrypted data keys.
type keyCache struct {
	entries map[string]*cacheEntry
	maxSize int
	mu      sync.RWMutex
}

type cacheEntry struct {
	expiresAt time.Time
	key       []byte
}

// NewService creates a new encryption service.
func NewService(config Config, provider kms.Provider) (*Service, error) {
	if provider == nil {
		return nil, errors.New("KMS provider is required")
	}

	if config.KeyCacheMaxSize == 0 {
		config.KeyCacheMaxSize = defaultKeyCacheMaxSize
	}

	if config.KeyCacheTTL == 0 {
		config.KeyCacheTTL = defaultKeyCacheTTL
	}

	return &Service{
		config:   config,
		provider: provider,
		cache: &keyCache{
			entries: make(map[string]*cacheEntry),
			maxSize: config.KeyCacheMaxSize,
		},
	}, nil
}

// EncryptedData holds the result of envelope encryption.
type EncryptedData struct {
	// KeyID is the KMS key used to encrypt the DEK
	KeyID string `json:"keyId"`

	// EncryptedDEK is the encrypted data encryption key
	EncryptedDEK []byte `json:"encryptedDek"`

	// Algorithm is the encryption algorithm used
	Algorithm kms.Algorithm `json:"algorithm"`

	// Ciphertext is the encrypted data
	Ciphertext []byte `json:"ciphertext"`
}

// Encrypt performs envelope encryption on the given data.
func (s *Service) Encrypt(ctx context.Context, plaintext []byte) (*EncryptedData, error) {
	return s.EncryptWithKey(ctx, s.config.DefaultKeyID, plaintext)
}

// EncryptWithKey performs envelope encryption using a specific KMS key.
func (s *Service) EncryptWithKey(ctx context.Context, keyID string, plaintext []byte) (*EncryptedData, error) {
	if keyID == "" {
		return nil, errors.New("key ID is required")
	}

	// Generate a new data encryption key (DEK)
	dataKey, err := s.provider.GenerateDataKey(ctx, keyID, kms.KeySpec{
		Algorithm: s.config.Algorithm,
	})
	if err != nil {
		return nil, err
	}

	// Encrypt the data with the DEK
	ciphertext, err := s.encryptData(dataKey.Plaintext, plaintext, s.config.Algorithm)
	if err != nil {
		return nil, err
	}

	// Clear plaintext DEK from memory
	clearBytes(dataKey.Plaintext)

	return &EncryptedData{
		KeyID:        keyID,
		EncryptedDEK: dataKey.Ciphertext,
		Algorithm:    s.config.Algorithm,
		Ciphertext:   ciphertext,
	}, nil
}

// Decrypt performs envelope decryption on the given encrypted data.
func (s *Service) Decrypt(ctx context.Context, data *EncryptedData) ([]byte, error) {
	if data == nil {
		return nil, errors.New("encrypted data is required")
	}

	// Check cache for DEK
	cacheKey := string(data.EncryptedDEK)
	if dek := s.cache.get(cacheKey); dek != nil {
		return s.decryptData(dek, data.Ciphertext, data.Algorithm)
	}

	// Decrypt the DEK using KMS
	dek, err := s.provider.DecryptDataKey(ctx, data.KeyID, data.EncryptedDEK)
	if err != nil {
		return nil, err
	}

	// Cache the DEK
	s.cache.set(cacheKey, dek, s.config.KeyCacheTTL)

	// Decrypt the data with the DEK
	plaintext, err := s.decryptData(dek, data.Ciphertext, data.Algorithm)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}

// encryptData encrypts data using the given key and algorithm.
func (s *Service) encryptData(key, plaintext []byte, algo kms.Algorithm) ([]byte, error) {
	switch algo {
	case kms.AlgorithmAES128, kms.AlgorithmAES256, kms.AlgorithmAES256GCM:
		return s.encryptAESGCM(key, plaintext)
	case kms.AlgorithmChaCha20:
		return s.encryptChaCha20(key, plaintext)
	default:
		return s.encryptAESGCM(key, plaintext)
	}
}

// decryptData decrypts data using the given key and algorithm.
func (s *Service) decryptData(key, ciphertext []byte, algo kms.Algorithm) ([]byte, error) {
	switch algo {
	case kms.AlgorithmAES128, kms.AlgorithmAES256, kms.AlgorithmAES256GCM:
		return s.decryptAESGCM(key, ciphertext)
	case kms.AlgorithmChaCha20:
		return s.decryptChaCha20(key, ciphertext)
	default:
		return s.decryptAESGCM(key, ciphertext)
	}
}

// encryptAESGCM encrypts data using AES-GCM.
func (s *Service) encryptAESGCM(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())

	_, readErr := io.ReadFull(rand.Reader, nonce)
	if readErr != nil {
		return nil, readErr
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptAESGCM decrypts data using AES-GCM.
func (s *Service) decryptAESGCM(key, ciphertext []byte) ([]byte, error) {
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

// encryptChaCha20 encrypts data using ChaCha20-Poly1305.
func (s *Service) encryptChaCha20(key, plaintext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())

	_, readErr := io.ReadFull(rand.Reader, nonce)
	if readErr != nil {
		return nil, readErr
	}

	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

// decryptChaCha20 decrypts data using ChaCha20-Poly1305.
func (s *Service) decryptChaCha20(key, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:aead.NonceSize()]
	ciphertext = ciphertext[aead.NonceSize():]

	return aead.Open(nil, nonce, ciphertext, nil)
}

// RotateKey re-encrypts data with a new data key.
func (s *Service) RotateKey(ctx context.Context, data *EncryptedData) (*EncryptedData, error) {
	// Decrypt the data
	plaintext, err := s.Decrypt(ctx, data)
	if err != nil {
		return nil, err
	}

	// Re-encrypt with a new DEK
	newData, err := s.EncryptWithKey(ctx, data.KeyID, plaintext)
	if err != nil {
		return nil, err
	}

	// Clear plaintext from memory
	clearBytes(plaintext)

	return newData, nil
}

// Provider returns the underlying KMS provider.
func (s *Service) Provider() kms.Provider {
	return s.provider
}

// Close closes the encryption service.
func (s *Service) Close() error {
	s.cache.clear()
	return nil
}

// keyCache methods

func (c *keyCache) get(key string) []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil
	}

	if time.Now().After(entry.expiresAt) {
		return nil
	}

	// Return a copy to prevent modification
	result := make([]byte, len(entry.key))
	copy(result, entry.key)

	return result
}

func (c *keyCache) set(key string, value []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict expired entries if at capacity
	if len(c.entries) >= c.maxSize {
		c.evictExpired()
	}

	// If still at capacity, evict oldest
	if len(c.entries) >= c.maxSize {
		c.evictOldest()
	}

	// Make a copy of the key
	keyCopy := make([]byte, len(value))
	copy(keyCopy, value)

	c.entries[key] = &cacheEntry{
		key:       keyCopy,
		expiresAt: time.Now().Add(ttl),
	}
}

func (c *keyCache) evictExpired() {
	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			clearBytes(entry.key)
			delete(c.entries, key)
		}
	}
}

func (c *keyCache) evictOldest() {
	var (
		oldestKey  string
		oldestTime time.Time
	)

	for key, entry := range c.entries {
		if oldestKey == "" || entry.expiresAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.expiresAt
		}
	}

	if oldestKey != "" {
		clearBytes(c.entries[oldestKey].key)
		delete(c.entries, oldestKey)
	}
}

func (c *keyCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range c.entries {
		clearBytes(entry.key)
	}

	c.entries = make(map[string]*cacheEntry)
}

// clearBytes securely clears a byte slice.
func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// StreamEncryptor provides streaming encryption for large files.
type StreamEncryptor struct {
	aead      cipher.AEAD
	service   *Service
	keyID     string
	algorithm kms.Algorithm
	dek       []byte
	encDEK    []byte
	nonce     []byte
	counter   uint64
}

// NewStreamEncryptor creates a new stream encryptor.
func (s *Service) NewStreamEncryptor(ctx context.Context, keyID string) (*StreamEncryptor, error) {
	if keyID == "" {
		keyID = s.config.DefaultKeyID
	}

	// Generate DEK
	dataKey, err := s.provider.GenerateDataKey(ctx, keyID, kms.KeySpec{
		Algorithm: s.config.Algorithm,
	})
	if err != nil {
		return nil, err
	}

	// Create AEAD
	var aead cipher.AEAD

	switch s.config.Algorithm {
	case kms.AlgorithmChaCha20:
		aead, err = chacha20poly1305.New(dataKey.Plaintext)
	default:
		var block cipher.Block

		block, err = aes.NewCipher(dataKey.Plaintext)
		if err != nil {
			return nil, err
		}

		aead, err = cipher.NewGCM(block)
	}

	if err != nil {
		return nil, err
	}

	// Generate random base nonce
	nonce := make([]byte, aead.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	if err != nil {
		return nil, err
	}

	return &StreamEncryptor{
		service:   s,
		keyID:     keyID,
		algorithm: s.config.Algorithm,
		dek:       dataKey.Plaintext,
		encDEK:    dataKey.Ciphertext,
		nonce:     nonce,
		counter:   0,
		aead:      aead,
	}, nil
}

// Header returns the encryption header to be stored with the encrypted data.
func (e *StreamEncryptor) Header() *EncryptedData {
	return &EncryptedData{
		KeyID:        e.keyID,
		EncryptedDEK: e.encDEK,
		Algorithm:    e.algorithm,
		Ciphertext:   e.nonce, // Store base nonce in ciphertext field of header
	}
}

// EncryptChunk encrypts a single chunk of data.
func (e *StreamEncryptor) EncryptChunk(plaintext []byte) ([]byte, error) {
	// Create counter-based nonce
	nonce := make([]byte, len(e.nonce))
	copy(nonce, e.nonce)

	// XOR counter into last 8 bytes of nonce
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, e.counter)

	for i := 0; i < 8 && i < len(nonce); i++ {
		nonce[len(nonce)-8+i] ^= counterBytes[i]
	}

	e.counter++

	return e.aead.Seal(nil, nonce, plaintext, nil), nil
}

// Close clears sensitive data from memory.
func (e *StreamEncryptor) Close() error {
	clearBytes(e.dek)
	return nil
}

// StreamDecryptor provides streaming decryption for large files.
type StreamDecryptor struct {
	aead      cipher.AEAD
	service   *Service
	algorithm kms.Algorithm
	dek       []byte
	nonce     []byte
	counter   uint64
}

// NewStreamDecryptor creates a new stream decryptor.
func (s *Service) NewStreamDecryptor(ctx context.Context, header *EncryptedData) (*StreamDecryptor, error) {
	// Get DEK from cache or KMS
	cacheKey := string(header.EncryptedDEK)

	dek := s.cache.get(cacheKey)
	if dek == nil {
		var err error

		dek, err = s.provider.DecryptDataKey(ctx, header.KeyID, header.EncryptedDEK)
		if err != nil {
			return nil, err
		}

		s.cache.set(cacheKey, dek, s.config.KeyCacheTTL)
	}

	// Create AEAD
	var (
		aead cipher.AEAD
		err  error
	)

	switch header.Algorithm {
	case kms.AlgorithmChaCha20:
		aead, err = chacha20poly1305.New(dek)
	default:
		var block cipher.Block

		block, err = aes.NewCipher(dek)
		if err != nil {
			return nil, err
		}

		aead, err = cipher.NewGCM(block)
	}

	if err != nil {
		return nil, err
	}

	return &StreamDecryptor{
		service:   s,
		algorithm: header.Algorithm,
		dek:       dek,
		nonce:     header.Ciphertext, // Base nonce stored in ciphertext field
		counter:   0,
		aead:      aead,
	}, nil
}

// DecryptChunk decrypts a single chunk of data.
func (d *StreamDecryptor) DecryptChunk(ciphertext []byte) ([]byte, error) {
	// Create counter-based nonce
	nonce := make([]byte, len(d.nonce))
	copy(nonce, d.nonce)

	// XOR counter into last 8 bytes of nonce
	counterBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(counterBytes, d.counter)

	for i := 0; i < 8 && i < len(nonce); i++ {
		nonce[len(nonce)-8+i] ^= counterBytes[i]
	}

	d.counter++

	return d.aead.Open(nil, nonce, ciphertext, nil)
}

// Close clears sensitive data from memory.
func (d *StreamDecryptor) Close() error {
	clearBytes(d.dek)
	return nil
}
