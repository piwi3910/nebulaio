package erasure

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/klauspost/reedsolomon"
)

// Decoder handles Reed-Solomon decoding and reconstruction operations
type Decoder struct {
	encoder      reedsolomon.Encoder
	dataShards   int
	parityShards int
}

// NewDecoder creates a new erasure decoder
func NewDecoder(dataShards, parityShards int) (*Decoder, error) {
	enc, err := reedsolomon.New(dataShards, parityShards)
	if err != nil {
		return nil, fmt.Errorf("failed to create decoder: %w", err)
	}

	return &Decoder{
		encoder:      enc,
		dataShards:   dataShards,
		parityShards: parityShards,
	}, nil
}

// ShardData represents a shard with its metadata
type ShardData struct {
	Index    int
	Data     []byte
	Checksum string
	Valid    bool
}

// DecodeInput contains shards for decoding
type DecodeInput struct {
	// Shards is the array of shards, with nil for missing ones
	Shards [][]byte

	// OriginalSize is the original size of the data (needed for trimming)
	OriginalSize int64

	// ExpectedChecksum is the expected SHA256 of the reconstructed data (optional)
	ExpectedChecksum string
}

// DecodeResult contains the reconstructed data
type DecodeResult struct {
	// Data is the reconstructed data
	Data []byte

	// Checksum is the SHA256 of the reconstructed data
	Checksum string

	// ReconstructedShards is the indices of shards that were reconstructed
	ReconstructedShards []int
}

// Decode reconstructs data from available shards
func (d *Decoder) Decode(input *DecodeInput) (*DecodeResult, error) {
	shards := input.Shards

	// Check if we have enough shards
	available := 0
	for _, shard := range shards {
		if shard != nil {
			available++
		}
	}

	if available < d.dataShards {
		return nil, fmt.Errorf("not enough shards for reconstruction: need %d, have %d", d.dataShards, available)
	}

	// Track which shards need reconstruction
	var reconstructed []int
	for i, shard := range shards {
		if shard == nil {
			reconstructed = append(reconstructed, i)
		}
	}

	// Reconstruct if needed
	if len(reconstructed) > 0 {
		if err := d.encoder.Reconstruct(shards); err != nil {
			return nil, fmt.Errorf("failed to reconstruct shards: %w", err)
		}
	}

	// Join data shards
	var buf bytes.Buffer
	for i := 0; i < d.dataShards; i++ {
		buf.Write(shards[i])
	}

	// Trim to original size
	data := buf.Bytes()
	if input.OriginalSize > 0 && int64(len(data)) > input.OriginalSize {
		data = data[:input.OriginalSize]
	}

	// Calculate checksum using SHA256 for better security
	hash := sha256.Sum256(data)
	checksum := hex.EncodeToString(hash[:])

	// Verify checksum if expected checksum is provided
	if input.ExpectedChecksum != "" && checksum != input.ExpectedChecksum {
		return nil, fmt.Errorf("checksum mismatch: expected %s, got %s", input.ExpectedChecksum, checksum)
	}

	return &DecodeResult{
		Data:                data,
		Checksum:            checksum,
		ReconstructedShards: reconstructed,
	}, nil
}

// DecodeToWriter reconstructs and writes data directly to a writer
func (d *Decoder) DecodeToWriter(input *DecodeInput, writer io.Writer) error {
	result, err := d.Decode(input)
	if err != nil {
		return err
	}

	_, err = writer.Write(result.Data)
	return err
}

// Verify checks if existing shards can reconstruct the original data
func (d *Decoder) Verify(shards [][]byte) (bool, error) {
	ok, err := d.encoder.Verify(shards)
	if err != nil {
		return false, fmt.Errorf("verification failed: %w", err)
	}
	return ok, nil
}

// VerifyAndRepair verifies shards and repairs any that are corrupted
func (d *Decoder) VerifyAndRepair(shards [][]byte, checksums []string) ([]int, error) {
	// First, identify corrupted shards by checksum
	corrupted := make([]int, 0)
	for i, shard := range shards {
		if shard == nil {
			corrupted = append(corrupted, i)
			continue
		}
		if i < len(checksums) && checksums[i] != "" {
			hash := sha256.Sum256(shard)
			if hex.EncodeToString(hash[:]) != checksums[i] {
				// Mark as corrupted by setting to nil
				shards[i] = nil
				corrupted = append(corrupted, i)
			}
		}
	}

	if len(corrupted) == 0 {
		return nil, nil // All shards are valid
	}

	// Check if we can repair
	valid := len(shards) - len(corrupted)
	if valid < d.dataShards {
		return corrupted, fmt.Errorf("too many corrupted shards: need %d valid, have %d", d.dataShards, valid)
	}

	// Repair corrupted shards
	if err := d.encoder.Reconstruct(shards); err != nil {
		return corrupted, fmt.Errorf("failed to repair shards: %w", err)
	}

	return corrupted, nil
}

// ShardsNeeded returns which shard indices are needed for reconstruction
// given a list of available shard indices
func (d *Decoder) ShardsNeeded(available []int) []int {
	if len(available) >= d.dataShards {
		return nil // Have enough
	}

	// Create a set of available indices
	avail := make(map[int]bool)
	for _, idx := range available {
		avail[idx] = true
	}

	// Find missing shards, prioritizing data shards
	var needed []int
	for i := 0; i < d.dataShards+d.parityShards; i++ {
		if !avail[i] {
			needed = append(needed, i)
		}
	}

	// We need (dataShards - len(available)) more shards
	toFetch := d.dataShards - len(available)
	if toFetch > len(needed) {
		toFetch = len(needed)
	}

	return needed[:toFetch]
}

// MinimumShards returns the minimum number of shards needed
func (d *Decoder) MinimumShards() int {
	return d.dataShards
}

// TotalShards returns the total number of shards
func (d *Decoder) TotalShards() int {
	return d.dataShards + d.parityShards
}
