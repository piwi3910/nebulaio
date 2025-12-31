package kms_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/piwi3910/nebulaio/internal/encryption"
	"github.com/piwi3910/nebulaio/internal/kms"
	"github.com/piwi3910/nebulaio/internal/kms/local"
	"github.com/piwi3910/nebulaio/internal/kms/vault"
)

func TestLocalProvider(t *testing.T) {
	// Create temp directory for keys
	tmpDir := t.TempDir()

	config := local.Config{
		ProviderConfig: kms.ProviderConfig{
			Type:        "local",
			Enabled:     true,
			KeyCacheTTL: 5 * time.Minute,
		},
		KeyStorePath:   tmpDir,
		MasterPassword: "test-password-123",
	}

	provider, err := local.NewProvider(config)
	if err != nil {
		t.Fatalf("Failed to create local provider: %v", err)
	}

	defer func() { _ = provider.Close() }()

	t.Run("Name", func(t *testing.T) { testLocalProviderName(t, provider) })
	t.Run("CreateKey", func(t *testing.T) { testLocalProviderCreateKey(t, provider) })
	t.Run("ListKeys", func(t *testing.T) { testLocalProviderListKeys(t, provider) })
	t.Run("GetKeyInfo", func(t *testing.T) { testLocalProviderGetKeyInfo(t, provider) })
	t.Run("GenerateDataKey", func(t *testing.T) { testLocalProviderGenerateDataKey(t, provider) })
	t.Run("DecryptDataKey", func(t *testing.T) { testLocalProviderDecryptDataKey(t, provider) })
	t.Run("EncryptDecrypt", func(t *testing.T) { testLocalProviderEncryptDecrypt(t, provider) })
	t.Run("RotateKey", func(t *testing.T) { testLocalProviderRotateKey(t, provider) })
	t.Run("DeleteKey", func(t *testing.T) { testLocalProviderDeleteKey(t, provider) })
	t.Run("KeyNotFound", func(t *testing.T) { testLocalProviderKeyNotFound(t, provider) })
	t.Run("KeyPersistence", func(t *testing.T) { testLocalProviderKeyPersistence(t, provider, config) })
}

func testLocalProviderName(t *testing.T, provider kms.Provider) {
	if provider.Name() != "local" {
		t.Errorf("Expected name 'local', got '%s'", provider.Name())
	}
}

func testLocalProviderCreateKey(t *testing.T, provider kms.Provider) {
	ctx := context.Background()

	keyInfo, err := provider.CreateKey(ctx, kms.KeySpec{
		Name:        "test-key-1",
		Description: "Test encryption key",
		Algorithm:   kms.AlgorithmAES256GCM,
		Usage:       kms.KeyUsageEncrypt,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	if keyInfo.KeyID == "" {
		t.Error("Expected non-empty key ID")
	}

	if keyInfo.Name != "test-key-1" {
		t.Errorf("Expected name 'test-key-1', got '%s'", keyInfo.Name)
	}

	if keyInfo.Algorithm != kms.AlgorithmAES256GCM {
		t.Errorf("Expected algorithm AES_256_GCM, got '%s'", keyInfo.Algorithm)
	}

	if keyInfo.State != kms.KeyStateEnabled {
		t.Errorf("Expected state ENABLED, got '%s'", keyInfo.State)
	}
}

func testLocalProviderListKeys(t *testing.T, provider kms.Provider) {
	ctx := context.Background()

	keys, err := provider.ListKeys(ctx)
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}

	if len(keys) == 0 {
		t.Error("Expected at least one key")
	}
}

func testLocalProviderGetKeyInfo(t *testing.T, provider kms.Provider) {
	ctx := context.Background()

	keys, err := provider.ListKeys(ctx)
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}

	if len(keys) == 0 {
		t.Skip("No keys to test")
	}

	keyInfo, err := provider.GetKeyInfo(ctx, keys[0].KeyID)
	if err != nil {
		t.Fatalf("Failed to get key info: %v", err)
	}

	if keyInfo.KeyID != keys[0].KeyID {
		t.Errorf("Key ID mismatch: expected '%s', got '%s'", keys[0].KeyID, keyInfo.KeyID)
	}
}

func testLocalProviderGenerateDataKey(t *testing.T, provider kms.Provider) {
	ctx := context.Background()

	keys, err := provider.ListKeys(ctx)
	if err != nil || len(keys) == 0 {
		t.Skip("No keys available")
	}

	dataKey, err := provider.GenerateDataKey(ctx, keys[0].KeyID, kms.KeySpec{
		Algorithm: kms.AlgorithmAES256GCM,
	})
	if err != nil {
		t.Fatalf("Failed to generate data key: %v", err)
	}

	if len(dataKey.Plaintext) != 32 {
		t.Errorf("Expected 32-byte plaintext, got %d bytes", len(dataKey.Plaintext))
	}

	if len(dataKey.Ciphertext) == 0 {
		t.Error("Expected non-empty ciphertext")
	}

	if dataKey.KeyID != keys[0].KeyID {
		t.Errorf("Key ID mismatch: expected '%s', got '%s'", keys[0].KeyID, dataKey.KeyID)
	}
}

func testLocalProviderDecryptDataKey(t *testing.T, provider kms.Provider) {
	ctx := context.Background()

	keys, err := provider.ListKeys(ctx)
	if err != nil || len(keys) == 0 {
		t.Skip("No keys available")
	}

	dataKey, err := provider.GenerateDataKey(ctx, keys[0].KeyID, kms.KeySpec{
		Algorithm: kms.AlgorithmAES256GCM,
	})
	if err != nil {
		t.Fatalf("Failed to generate data key: %v", err)
	}

	decrypted, err := provider.DecryptDataKey(ctx, keys[0].KeyID, dataKey.Ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt data key: %v", err)
	}

	if string(decrypted) != string(dataKey.Plaintext) {
		t.Error("Decrypted key does not match original plaintext")
	}
}

func testLocalProviderEncryptDecrypt(t *testing.T, provider kms.Provider) {
	ctx := context.Background()

	keys, err := provider.ListKeys(ctx)
	if err != nil || len(keys) == 0 {
		t.Skip("No keys available")
	}

	plaintext := []byte("Hello, World! This is a test message.")

	ciphertext, err := provider.Encrypt(ctx, keys[0].KeyID, plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	if string(ciphertext) == string(plaintext) {
		t.Error("Ciphertext should not equal plaintext")
	}

	decrypted, err := provider.Decrypt(ctx, keys[0].KeyID, ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("Decrypted text does not match original plaintext")
	}
}

func testLocalProviderRotateKey(t *testing.T, provider kms.Provider) {
	ctx := context.Background()

	keys, err := provider.ListKeys(ctx)
	if err != nil || len(keys) == 0 {
		t.Skip("No keys available")
	}

	originalInfo, err := provider.GetKeyInfo(ctx, keys[0].KeyID)
	if err != nil {
		t.Fatalf("Failed to get key info: %v", err)
	}

	rotatedInfo, err := provider.RotateKey(ctx, keys[0].KeyID)
	if err != nil {
		t.Fatalf("Failed to rotate key: %v", err)
	}

	if rotatedInfo.RotatedAt == nil {
		t.Error("Expected RotatedAt to be set after rotation")
	}

	if originalInfo.RotatedAt != nil && rotatedInfo.RotatedAt.Before(*originalInfo.RotatedAt) {
		t.Error("RotatedAt should be after previous rotation time")
	}
}

func testLocalProviderDeleteKey(t *testing.T, provider kms.Provider) {
	ctx := context.Background()

	// Create a key to delete
	keyInfo, err := provider.CreateKey(ctx, kms.KeySpec{
		Name:      "key-to-delete",
		Algorithm: kms.AlgorithmAES256GCM,
		Usage:     kms.KeyUsageEncrypt,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	err = provider.DeleteKey(ctx, keyInfo.KeyID)
	if err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}

	// Key should be marked as pending deletion
	info, err := provider.GetKeyInfo(ctx, keyInfo.KeyID)
	if err != nil {
		t.Fatalf("Failed to get key info: %v", err)
	}

	if info.State != kms.KeyStatePendingDeletion {
		t.Errorf("Expected state PENDING_DELETION, got '%s'", info.State)
	}
}

func testLocalProviderKeyNotFound(t *testing.T, provider kms.Provider) {
	ctx := context.Background()

	_, err := provider.GetKeyInfo(ctx, "non-existent-key")
	if err != kms.ErrKeyNotFound {
		t.Errorf("Expected ErrKeyNotFound, got %v", err)
	}
}

func testLocalProviderKeyPersistence(t *testing.T, provider kms.Provider, config local.Config) {
	ctx := context.Background()

	// Create a key
	keyInfo, err := provider.CreateKey(ctx, kms.KeySpec{
		Name:        "persistent-key",
		Description: "Test persistence",
		Algorithm:   kms.AlgorithmAES256GCM,
		Usage:       kms.KeyUsageEncrypt,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Encrypt some data
	plaintext := []byte("Test data for persistence")

	ciphertext, err := provider.Encrypt(ctx, keyInfo.KeyID, plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	// Close the provider
	_ = provider.Close()

	// Create a new provider with the same config
	newProvider, err := local.NewProvider(config)
	if err != nil {
		t.Fatalf("Failed to create new provider: %v", err)
	}

	defer func() { _ = newProvider.Close() }()

	// Verify the key still exists
	loadedInfo, err := newProvider.GetKeyInfo(ctx, keyInfo.KeyID)
	if err != nil {
		t.Fatalf("Failed to get key info from new provider: %v", err)
	}

	if loadedInfo.KeyID != keyInfo.KeyID {
		t.Error("Key ID changed after reload")
	}

	// Verify we can still decrypt
	decrypted, err := newProvider.Decrypt(ctx, keyInfo.KeyID, ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt with new provider: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("Decrypted data does not match original")
	}
}

func TestVaultProvider(t *testing.T) {
	// Use mock client for testing
	mockClient := vault.NewMockVaultClient()

	config := vault.Config{
		ProviderConfig: kms.ProviderConfig{
			Type:        "vault",
			Enabled:     true,
			KeyCacheTTL: 5 * time.Minute,
		},
		Address:   "http://127.0.0.1:8200",
		MountPath: "transit",
	}

	provider, err := vault.NewProvider(config, mockClient)
	if err != nil {
		t.Fatalf("Failed to create vault provider: %v", err)
	}

	defer func() { _ = provider.Close() }()

	t.Run("Name", func(t *testing.T) { testVaultProviderName(t, provider) })
	t.Run("CreateKey", func(t *testing.T) { testVaultProviderCreateKey(t, provider) })
	t.Run("ListKeys", func(t *testing.T) { testVaultProviderListKeys(t, provider) })
	t.Run("EncryptDecrypt", func(t *testing.T) { testVaultProviderEncryptDecrypt(t, provider) })
	t.Run("GenerateDecryptDataKey", func(t *testing.T) { testVaultProviderGenerateDecryptDataKey(t, provider) })
	t.Run("RotateKey", func(t *testing.T) { testVaultProviderRotateKey(t, provider) })
	t.Run("DeleteKey", func(t *testing.T) { testVaultProviderDeleteKey(t, provider) })
}

func testVaultProviderName(t *testing.T, provider *vault.Provider) {
	if provider.Name() != "vault" {
		t.Errorf("Expected name 'vault', got '%s'", provider.Name())
	}
}

func testVaultProviderCreateKey(t *testing.T, provider *vault.Provider) {
	ctx := context.Background()

	keyInfo, err := provider.CreateKey(ctx, kms.KeySpec{
		Name:      "test-vault-key",
		Algorithm: kms.AlgorithmAES256GCM,
		Usage:     kms.KeyUsageEncrypt,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	if keyInfo.KeyID != "test-vault-key" {
		t.Errorf("Expected key ID 'test-vault-key', got '%s'", keyInfo.KeyID)
	}
}

func testVaultProviderListKeys(t *testing.T, provider *vault.Provider) {
	ctx := context.Background()

	keys, err := provider.ListKeys(ctx)
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}

	if len(keys) == 0 {
		t.Error("Expected at least one key")
	}
}

func testVaultProviderEncryptDecrypt(t *testing.T, provider *vault.Provider) {
	ctx := context.Background()

	plaintext := []byte("Hello from Vault!")

	ciphertext, err := provider.Encrypt(ctx, "test-vault-key", plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	decrypted, err := provider.Decrypt(ctx, "test-vault-key", ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("Decrypted text does not match plaintext")
	}
}

func testVaultProviderGenerateDecryptDataKey(t *testing.T, provider *vault.Provider) {
	ctx := context.Background()

	dataKey, err := provider.GenerateDataKey(ctx, "test-vault-key", kms.KeySpec{
		Algorithm: kms.AlgorithmAES256GCM,
	})
	if err != nil {
		t.Fatalf("Failed to generate data key: %v", err)
	}

	if len(dataKey.Plaintext) == 0 {
		t.Error("Expected non-empty plaintext")
	}

	decrypted, err := provider.DecryptDataKey(ctx, "test-vault-key", dataKey.Ciphertext)
	if err != nil {
		t.Fatalf("Failed to decrypt data key: %v", err)
	}

	if string(decrypted) != string(dataKey.Plaintext) {
		t.Error("Decrypted key does not match plaintext")
	}
}

func testVaultProviderRotateKey(t *testing.T, provider *vault.Provider) {
	ctx := context.Background()

	_, err := provider.RotateKey(ctx, "test-vault-key")
	if err != nil {
		t.Fatalf("Failed to rotate key: %v", err)
	}
}

func testVaultProviderDeleteKey(t *testing.T, provider *vault.Provider) {
	ctx := context.Background()

	err := provider.DeleteKey(ctx, "test-vault-key")
	if err != nil {
		t.Fatalf("Failed to delete key: %v", err)
	}
}

func TestEncryptionService(t *testing.T) {
	// Create temp directory for keys
	tmpDir := t.TempDir()

	// Create local KMS provider
	kmsConfig := local.Config{
		ProviderConfig: kms.ProviderConfig{
			Type:        "local",
			Enabled:     true,
			KeyCacheTTL: 5 * time.Minute,
		},
		KeyStorePath:   tmpDir,
		MasterPassword: "test-password",
	}

	provider, err := local.NewProvider(kmsConfig)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	defer func() { _ = provider.Close() }()

	// Create a key
	ctx := context.Background()

	keyInfo, err := provider.CreateKey(ctx, kms.KeySpec{
		Name:      "test-encryption-key",
		Algorithm: kms.AlgorithmAES256GCM,
		Usage:     kms.KeyUsageEncrypt,
	})
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Create encryption service
	svcConfig := encryption.Config{
		DefaultKeyID:    keyInfo.KeyID,
		Algorithm:       kms.AlgorithmAES256GCM,
		KeyCacheTTL:     5 * time.Minute,
		KeyCacheMaxSize: 100,
	}

	service, err := encryption.NewService(svcConfig, provider)
	if err != nil {
		t.Fatalf("Failed to create encryption service: %v", err)
	}

	defer func() { _ = service.Close() }()

	t.Run("EncryptDecrypt", func(t *testing.T) { testEncryptionServiceEncryptDecrypt(t, ctx, service, keyInfo) })
	t.Run("EncryptWithKey", func(t *testing.T) { testEncryptionServiceEncryptWithKey(t, ctx, service, keyInfo) })
	t.Run("RotateKey", func(t *testing.T) { testEncryptionServiceRotateKey(t, ctx, service) })
	t.Run("LargeData", func(t *testing.T) { testEncryptionServiceLargeData(t, ctx, service) })
	t.Run("StreamEncryption", func(t *testing.T) { testEncryptionServiceStreamEncryption(t, ctx, service, keyInfo) })
	t.Run("ChaCha20Encryption", func(t *testing.T) { testEncryptionServiceChaCha20(t, ctx, provider) })
}

func testEncryptionServiceEncryptDecrypt(t *testing.T, ctx context.Context, service *encryption.Service, keyInfo *kms.KeyInfo) {
	plaintext := []byte("Hello, encryption service!")

	encrypted, err := service.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	if encrypted.KeyID != keyInfo.KeyID {
		t.Errorf("Key ID mismatch: expected '%s', got '%s'", keyInfo.KeyID, encrypted.KeyID)
	}

	if len(encrypted.EncryptedDEK) == 0 {
		t.Error("Expected non-empty encrypted DEK")
	}

	if len(encrypted.Ciphertext) == 0 {
		t.Error("Expected non-empty ciphertext")
	}

	decrypted, err := service.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("Decrypted text does not match plaintext")
	}
}

func testEncryptionServiceEncryptWithKey(t *testing.T, ctx context.Context, service *encryption.Service, keyInfo *kms.KeyInfo) {
	plaintext := []byte("Using explicit key ID")

	encrypted, err := service.EncryptWithKey(ctx, keyInfo.KeyID, plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	decrypted, err := service.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("Decrypted text does not match plaintext")
	}
}

func testEncryptionServiceRotateKey(t *testing.T, ctx context.Context, service *encryption.Service) {
	plaintext := []byte("Data to rotate")

	encrypted, err := service.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	rotated, err := service.RotateKey(ctx, encrypted)
	if err != nil {
		t.Fatalf("Failed to rotate: %v", err)
	}

	// New encrypted DEK should be different
	if string(rotated.EncryptedDEK) == string(encrypted.EncryptedDEK) {
		t.Error("Encrypted DEK should change after rotation")
	}

	// Should still decrypt to same plaintext
	decrypted, err := service.Decrypt(ctx, rotated)
	if err != nil {
		t.Fatalf("Failed to decrypt rotated data: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("Decrypted text does not match plaintext after rotation")
	}
}

func testEncryptionServiceLargeData(t *testing.T, ctx context.Context, service *encryption.Service) {
	// Test with 1MB of data
	plaintext := make([]byte, 1024*1024)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	encrypted, err := service.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt large data: %v", err)
	}

	decrypted, err := service.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt large data: %v", err)
	}

	if len(decrypted) != len(plaintext) {
		t.Errorf("Length mismatch: expected %d, got %d", len(plaintext), len(decrypted))
	}

	for i := range plaintext {
		if plaintext[i] != decrypted[i] {
			t.Errorf("Data mismatch at position %d", i)
			break
		}
	}
}

func testEncryptionServiceStreamEncryption(t *testing.T, ctx context.Context, service *encryption.Service, keyInfo *kms.KeyInfo) {
	// Create stream encryptor
	encryptor, err := service.NewStreamEncryptor(ctx, keyInfo.KeyID)
	if err != nil {
		t.Fatalf("Failed to create stream encryptor: %v", err)
	}

	defer func() { _ = encryptor.Close() }()

	// Encrypt chunks
	chunks := [][]byte{
		[]byte("First chunk of data"),
		[]byte("Second chunk of data"),
		[]byte("Third chunk of data"),
	}

	header := encryptor.Header()
	encryptedChunks := make([][]byte, len(chunks))

	for i, chunk := range chunks {
		encrypted, err := encryptor.EncryptChunk(chunk)
		if err != nil {
			t.Fatalf("Failed to encrypt chunk %d: %v", i, err)
		}

		encryptedChunks[i] = encrypted
	}

	// Create stream decryptor
	decryptor, err := service.NewStreamDecryptor(ctx, header)
	if err != nil {
		t.Fatalf("Failed to create stream decryptor: %v", err)
	}

	defer func() { _ = decryptor.Close() }()

	// Decrypt chunks
	for i, encChunk := range encryptedChunks {
		decrypted, err := decryptor.DecryptChunk(encChunk)
		if err != nil {
			t.Fatalf("Failed to decrypt chunk %d: %v", i, err)
		}

		if string(decrypted) != string(chunks[i]) {
			t.Errorf("Chunk %d mismatch: expected '%s', got '%s'", i, chunks[i], decrypted)
		}
	}
}

func testEncryptionServiceChaCha20(t *testing.T, ctx context.Context, provider kms.Provider) {
	// Create a ChaCha20 key
	chachaKey, err := provider.CreateKey(ctx, kms.KeySpec{
		Name:      "chacha20-key",
		Algorithm: kms.AlgorithmChaCha20,
		Usage:     kms.KeyUsageEncrypt,
	})
	if err != nil {
		t.Fatalf("Failed to create ChaCha20 key: %v", err)
	}

	// Create service with ChaCha20
	chachaSvc, err := encryption.NewService(encryption.Config{
		DefaultKeyID:    chachaKey.KeyID,
		Algorithm:       kms.AlgorithmChaCha20,
		KeyCacheTTL:     5 * time.Minute,
		KeyCacheMaxSize: 100,
	}, provider)
	if err != nil {
		t.Fatalf("Failed to create ChaCha20 service: %v", err)
	}

	defer func() { _ = chachaSvc.Close() }()

	plaintext := []byte("Testing ChaCha20-Poly1305")

	encrypted, err := chachaSvc.Encrypt(ctx, plaintext)
	if err != nil {
		t.Fatalf("Failed to encrypt with ChaCha20: %v", err)
	}

	if encrypted.Algorithm != kms.AlgorithmChaCha20 {
		t.Errorf("Expected ChaCha20 algorithm, got %s", encrypted.Algorithm)
	}

	decrypted, err := chachaSvc.Decrypt(ctx, encrypted)
	if err != nil {
		t.Fatalf("Failed to decrypt with ChaCha20: %v", err)
	}

	if string(decrypted) != string(plaintext) {
		t.Error("ChaCha20 decryption failed")
	}
}

func TestProviderErrors(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	config := local.Config{
		ProviderConfig: kms.ProviderConfig{
			Type:    "local",
			Enabled: true,
		},
		KeyStorePath:   tmpDir,
		MasterPassword: "test",
	}

	provider, err := local.NewProvider(config)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	ctx := context.Background()

	t.Run("EncryptWithDisabledKey", func(t *testing.T) {
		// Create and delete a key
		keyInfo, err := provider.CreateKey(ctx, kms.KeySpec{
			Name:      "disabled-key",
			Algorithm: kms.AlgorithmAES256GCM,
			Usage:     kms.KeyUsageEncrypt,
		})
		if err != nil {
			t.Fatalf("Failed to create key: %v", err)
		}

		// Delete key (marks as pending deletion)
		err = provider.DeleteKey(ctx, keyInfo.KeyID)
		if err != nil {
			t.Fatalf("Failed to delete key: %v", err)
		}

		// Try to encrypt - should fail
		_, err = provider.Encrypt(ctx, keyInfo.KeyID, []byte("test"))
		if err == nil {
			t.Error("Expected error when encrypting with deleted key")
		}
	})

	t.Run("ProviderClosed", func(t *testing.T) {
		_ = provider.Close()

		_, err := provider.ListKeys(ctx)
		if err != kms.ErrProviderClosed {
			t.Errorf("Expected ErrProviderClosed, got %v", err)
		}
	})
}

func TestKeyFileStorage(t *testing.T) {
	t.Run("WithPassword", func(t *testing.T) {
		tmpDir := t.TempDir()

		config := local.Config{
			ProviderConfig: kms.ProviderConfig{
				Type:    "local",
				Enabled: true,
			},
			KeyStorePath:   tmpDir,
			MasterPassword: "test",
		}

		provider, err := local.NewProvider(config)
		if err != nil {
			t.Fatalf("Failed to create provider: %v", err)
		}

		ctx := context.Background()

		// Create a key
		keyInfo, err := provider.CreateKey(ctx, kms.KeySpec{
			Name:        "storage-test",
			Description: "Test storage",
			Algorithm:   kms.AlgorithmAES256GCM,
			Usage:       kms.KeyUsageEncrypt,
		})
		if err != nil {
			t.Fatalf("Failed to create key: %v", err)
		}

		_ = provider.Close()

		// Verify key file exists
		keyPath := filepath.Join(tmpDir, keyInfo.KeyID+".key")

		_, statErr := os.Stat(keyPath)
		if os.IsNotExist(statErr) {
			t.Error("Key file should exist")
		}

		// When password is provided, master key file should NOT exist
		masterPath := filepath.Join(tmpDir, ".master.key")

		_, masterStatErr := os.Stat(masterPath)
		if !os.IsNotExist(masterStatErr) {
			t.Error("Master key file should NOT exist when password is provided")
		}
	})

	t.Run("WithoutPassword", func(t *testing.T) {
		tmpDir := t.TempDir()

		config := local.Config{
			ProviderConfig: kms.ProviderConfig{
				Type:    "local",
				Enabled: true,
			},
			KeyStorePath: tmpDir,
			// No MasterPassword - should generate and save master key
		}

		provider, err := local.NewProvider(config)
		if err != nil {
			t.Fatalf("Failed to create provider: %v", err)
		}

		ctx := context.Background()

		// Create a key
		_, err = provider.CreateKey(ctx, kms.KeySpec{
			Name:        "storage-test",
			Description: "Test storage",
			Algorithm:   kms.AlgorithmAES256GCM,
			Usage:       kms.KeyUsageEncrypt,
		})
		if err != nil {
			t.Fatalf("Failed to create key: %v", err)
		}

		_ = provider.Close()

		// Verify master key file exists when no password provided
		masterPath := filepath.Join(tmpDir, ".master.key")
		_, err = os.Stat(masterPath)
		if os.IsNotExist(err) {
			t.Error("Master key file should exist when no password provided")
		}
	})
}
