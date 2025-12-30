package erasure

import (
	"bytes"
	"context"
	"crypto/rand"
	"testing"
)

func TestConfig(t *testing.T) {
	t.Run("DefaultConfig", func(t *testing.T) {
		cfg := DefaultConfig()
		if cfg.DataShards != 10 {
			t.Errorf("expected DataShards=10, got %d", cfg.DataShards)
		}

		if cfg.ParityShards != 4 {
			t.Errorf("expected ParityShards=4, got %d", cfg.ParityShards)
		}
	})

	t.Run("ConfigFromPreset", func(t *testing.T) {
		tests := []struct {
			preset       Preset
			dataShards   int
			parityShards int
		}{
			{PresetMinimal, 4, 2},
			{PresetStandard, 10, 4},
			{PresetMaximum, 8, 8},
		}

		for _, tt := range tests {
			cfg := ConfigFromPreset(tt.preset, "/data")
			if cfg.DataShards != tt.dataShards {
				t.Errorf("preset %s: expected DataShards=%d, got %d", tt.preset, tt.dataShards, cfg.DataShards)
			}

			if cfg.ParityShards != tt.parityShards {
				t.Errorf("preset %s: expected ParityShards=%d, got %d", tt.preset, tt.parityShards, cfg.ParityShards)
			}
		}
	})

	t.Run("Validate", func(t *testing.T) {
		cfg := DefaultConfig()

		cfg.DataDir = "/data"

		err := cfg.Validate()
		if err != nil {
			t.Errorf("valid config should not error: %v", err)
		}

		// Test invalid configs
		invalid := cfg

		invalid.DataShards = 0

		err = invalid.Validate()
		if err == nil {
			t.Error("expected error for DataShards=0")
		}

		invalid = cfg

		invalid.ParityShards = 0

		err = invalid.Validate()
		if err == nil {
			t.Error("expected error for ParityShards=0")
		}

		invalid = cfg

		invalid.DataDir = ""

		err = invalid.Validate()
		if err == nil {
			t.Error("expected error for empty DataDir")
		}
	})
}

func TestEncoder(t *testing.T) {
	enc, err := NewEncoder(4, 2, 1024*1024)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	t.Run("EncodeSmallData", func(t *testing.T) {
		data := []byte("Hello, World!")

		encoded, err := enc.Encode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		if len(encoded.Shards) != 6 {
			t.Errorf("expected 6 shards, got %d", len(encoded.Shards))
		}

		if encoded.OriginalSize != int64(len(data)) {
			t.Errorf("expected size %d, got %d", len(data), encoded.OriginalSize)
		}
	})

	t.Run("EncodeLargeData", func(t *testing.T) {
		data := make([]byte, 1024*1024) // 1MB
		_, _ = rand.Read(data)

		encoded, err := enc.Encode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		if len(encoded.Shards) != 6 {
			t.Errorf("expected 6 shards, got %d", len(encoded.Shards))
		}

		if encoded.OriginalSize != int64(len(data)) {
			t.Errorf("expected size %d, got %d", len(data), encoded.OriginalSize)
		}
	})
}

func TestDecoder(t *testing.T) {
	dataShards := 4
	parityShards := 2

	enc, err := NewEncoder(dataShards, parityShards, 1024*1024)
	if err != nil {
		t.Fatalf("failed to create encoder: %v", err)
	}

	dec, err := NewDecoder(dataShards, parityShards)
	if err != nil {
		t.Fatalf("failed to create decoder: %v", err)
	}

	t.Run("DecodeComplete", func(t *testing.T) {
		data := []byte("Test data for decoding")

		encoded, err := enc.Encode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		result, err := dec.Decode(&DecodeInput{
			Shards:       encoded.Shards,
			OriginalSize: encoded.OriginalSize,
		})
		if err != nil {
			t.Fatalf("failed to decode: %v", err)
		}

		if !bytes.Equal(result.Data, data) {
			t.Error("decoded data doesn't match original")
		}
	})

	t.Run("DecodeWithMissingShard", func(t *testing.T) {
		data := []byte("Test data for partial decode")

		encoded, err := enc.Encode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		// Remove one shard
		encoded.Shards[0] = nil

		result, err := dec.Decode(&DecodeInput{
			Shards:       encoded.Shards,
			OriginalSize: encoded.OriginalSize,
		})
		if err != nil {
			t.Fatalf("failed to decode with 1 missing shard: %v", err)
		}

		if !bytes.Equal(result.Data, data) {
			t.Error("decoded data doesn't match original")
		}

		if len(result.ReconstructedShards) != 1 {
			t.Errorf("expected 1 reconstructed shard, got %d", len(result.ReconstructedShards))
		}
	})

	t.Run("DecodeWithMaxMissing", func(t *testing.T) {
		data := []byte("Test data for max missing")

		encoded, err := enc.Encode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		// Remove parityShards shards (maximum allowed)
		for i := range parityShards {
			encoded.Shards[i] = nil
		}

		result, err := dec.Decode(&DecodeInput{
			Shards:       encoded.Shards,
			OriginalSize: encoded.OriginalSize,
		})
		if err != nil {
			t.Fatalf("failed to decode with max missing shards: %v", err)
		}

		if !bytes.Equal(result.Data, data) {
			t.Error("decoded data doesn't match original")
		}
	})

	t.Run("DecodeFailsWithTooManyMissing", func(t *testing.T) {
		data := []byte("Test data for failure case")

		encoded, err := enc.Encode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("failed to encode: %v", err)
		}

		// Remove more than parityShards
		for i := 0; i <= parityShards; i++ {
			encoded.Shards[i] = nil
		}

		_, err = dec.Decode(&DecodeInput{
			Shards:       encoded.Shards,
			OriginalSize: encoded.OriginalSize,
		})
		if err == nil {
			t.Error("expected decode to fail with too many missing shards")
		}
	})
}

func TestPlacement(t *testing.T) {
	nodes := []NodeInfo{
		{ID: "node1", Address: "192.168.1.1:9000", Status: "alive"},
		{ID: "node2", Address: "192.168.1.2:9000", Status: "alive"},
		{ID: "node3", Address: "192.168.1.3:9000", Status: "alive"},
		{ID: "node4", Address: "192.168.1.4:9000", Status: "alive"},
	}

	t.Run("ConsistentHashPlacement", func(t *testing.T) {
		placement := NewConsistentHashPlacement(100)

		assignments, err := placement.PlaceShards("bucket/key", 6, nodes)
		if err != nil {
			t.Fatalf("failed to place shards: %v", err)
		}

		if len(assignments) != 6 {
			t.Errorf("expected 6 assignments, got %d", len(assignments))
		}

		// Verify determinism - same key should give same placement
		assignments2, err := placement.PlaceShards("bucket/key", 6, nodes)
		if err != nil {
			t.Fatalf("failed to place shards: %v", err)
		}

		for i := range assignments {
			if assignments[i].NodeID != assignments2[i].NodeID {
				t.Error("placement should be deterministic")
			}
		}
	})

	t.Run("RoundRobinPlacement", func(t *testing.T) {
		placement := NewRoundRobinPlacement()

		assignments, err := placement.PlaceShards("bucket/key", 6, nodes)
		if err != nil {
			t.Fatalf("failed to place shards: %v", err)
		}

		if len(assignments) != 6 {
			t.Errorf("expected 6 assignments, got %d", len(assignments))
		}

		// Verify round-robin distribution
		for i, a := range assignments {
			expectedNode := nodes[i%len(nodes)]
			if a.NodeID != expectedNode.ID {
				t.Errorf("shard %d: expected node %s, got %s", i, expectedNode.ID, a.NodeID)
			}
		}
	})

	t.Run("FilterDeadNodes", func(t *testing.T) {
		nodesWithDead := []NodeInfo{
			{ID: "node1", Address: "192.168.1.1:9000", Status: "alive"},
			{ID: "node2", Address: "192.168.1.2:9000", Status: "dead"},
			{ID: "node3", Address: "192.168.1.3:9000", Status: "alive"},
		}

		placement := NewRoundRobinPlacement()

		assignments, err := placement.PlaceShards("bucket/key", 4, nodesWithDead)
		if err != nil {
			t.Fatalf("failed to place shards: %v", err)
		}

		for _, a := range assignments {
			if a.NodeID == "node2" {
				t.Error("dead node should not be assigned shards")
			}
		}
	})
}

func TestShardManager(t *testing.T) {
	tmpDir := t.TempDir()

	mgr, err := NewShardManager(tmpDir)
	if err != nil {
		t.Fatalf("failed to create shard manager: %v", err)
	}

	ctx := context.Background()

	t.Run("WriteAndReadShard", func(t *testing.T) {
		data := []byte("test shard data")

		path, err := mgr.WriteShard(ctx, "bucket", "key", 0, data)
		if err != nil {
			t.Fatalf("failed to write shard: %v", err)
		}

		if path == "" {
			t.Error("expected non-empty path")
		}

		readData, err := mgr.ReadShard(ctx, "bucket", "key", 0)
		if err != nil {
			t.Fatalf("failed to read shard: %v", err)
		}

		if !bytes.Equal(data, readData) {
			t.Error("read data doesn't match written data")
		}
	})

	t.Run("ShardExists", func(t *testing.T) {
		if !mgr.ShardExists(ctx, "bucket", "key", 0) {
			t.Error("shard should exist")
		}

		if mgr.ShardExists(ctx, "bucket", "key", 999) {
			t.Error("non-existent shard should not exist")
		}
	})

	t.Run("DeleteShard", func(t *testing.T) {
		err := mgr.DeleteShard(ctx, "bucket", "key", 0)
		if err != nil {
			t.Fatalf("failed to delete shard: %v", err)
		}

		if mgr.ShardExists(ctx, "bucket", "key", 0) {
			t.Error("shard should not exist after deletion")
		}
	})
}

func TestBackend(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		DataShards:   4,
		ParityShards: 2,
		ShardSize:    1024 * 1024,
		DataDir:      tmpDir,
	}

	backend, err := New(cfg, "test-node")
	if err != nil {
		t.Fatalf("failed to create backend: %v", err)
	}

	ctx := context.Background()

	if err := backend.Init(ctx); err != nil {
		t.Fatalf("failed to init backend: %v", err)
	}

	t.Run("PutAndGetObject", func(t *testing.T) {
		data := []byte("Test object data for erasure coding backend")

		result, err := backend.PutObject(ctx, "test-bucket", "test-key", bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("failed to put object: %v", err)
		}

		if result.ETag == "" {
			t.Error("expected non-empty ETag")
		}

		reader, err := backend.GetObject(ctx, "test-bucket", "test-key")
		if err != nil {
			t.Fatalf("failed to get object: %v", err)
		}

		defer func() { _ = reader.Close() }()

		readData := make([]byte, len(data))

		n, err := reader.Read(readData)
		if err != nil {
			t.Fatalf("failed to read object: %v", err)
		}

		if !bytes.Equal(data, readData[:n]) {
			t.Error("read data doesn't match written data")
		}
	})

	t.Run("ObjectExists", func(t *testing.T) {
		exists, err := backend.ObjectExists(ctx, "test-bucket", "test-key")
		if err != nil {
			t.Fatalf("failed to check existence: %v", err)
		}

		if !exists {
			t.Error("object should exist")
		}

		exists, err = backend.ObjectExists(ctx, "test-bucket", "non-existent")
		if err != nil {
			t.Fatalf("failed to check existence: %v", err)
		}

		if exists {
			t.Error("non-existent object should not exist")
		}
	})

	t.Run("DeleteObject", func(t *testing.T) {
		err := backend.DeleteObject(ctx, "test-bucket", "test-key")
		if err != nil {
			t.Fatalf("failed to delete object: %v", err)
		}

		exists, _ := backend.ObjectExists(ctx, "test-bucket", "test-key")
		if exists {
			t.Error("object should not exist after deletion")
		}
	})

	t.Run("LargeObject", func(t *testing.T) {
		data := make([]byte, 10*1024*1024) // 10MB
		_, _ = rand.Read(data)

		_, err := backend.PutObject(ctx, "test-bucket", "large-key", bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("failed to put large object: %v", err)
		}

		reader, err := backend.GetObject(ctx, "test-bucket", "large-key")
		if err != nil {
			t.Fatalf("failed to get large object: %v", err)
		}

		defer func() { _ = reader.Close() }()

		var buf bytes.Buffer

		_, err = buf.ReadFrom(reader)
		if err != nil {
			t.Fatalf("failed to read large object: %v", err)
		}

		if !bytes.Equal(data, buf.Bytes()) {
			t.Error("large object data mismatch")
		}
	})
}

func BenchmarkEncode(b *testing.B) {
	enc, _ := NewEncoder(10, 4, 1024*1024)
	data := make([]byte, 10*1024*1024) // 10MB
	_, _ = rand.Read(data)

	b.ResetTimer()

	for range b.N {
		_, _ = enc.Encode(bytes.NewReader(data))
	}
}

func BenchmarkDecode(b *testing.B) {
	enc, _ := NewEncoder(10, 4, 1024*1024)
	dec, _ := NewDecoder(10, 4)
	data := make([]byte, 10*1024*1024) // 10MB
	_, _ = rand.Read(data)

	encoded, _ := enc.Encode(bytes.NewReader(data))

	b.ResetTimer()

	for range b.N {
		_, _ = dec.Decode(&DecodeInput{
			Shards:       encoded.Shards,
			OriginalSize: encoded.OriginalSize,
		})
	}
}
