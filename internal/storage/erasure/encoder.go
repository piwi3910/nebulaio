package erasure

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/klauspost/reedsolomon"
)

// Encoder handles Reed-Solomon encoding operations
type Encoder struct {
	encoder      reedsolomon.Encoder
	dataShards   int
	parityShards int
	shardSize    int
}

// NewEncoder creates a new erasure encoder
func NewEncoder(dataShards, parityShards, shardSize int) (*Encoder, error) {
	enc, err := reedsolomon.New(dataShards, parityShards)
	if err != nil {
		return nil, fmt.Errorf("failed to create encoder: %w", err)
	}

	return &Encoder{
		encoder:      enc,
		dataShards:   dataShards,
		parityShards: parityShards,
		shardSize:    shardSize,
	}, nil
}

// EncodedData represents the result of encoding an object
type EncodedData struct {
	// Shards contains all data and parity shards
	Shards [][]byte

	// ShardChecksums contains MD5 checksums for each shard
	ShardChecksums []string

	// OriginalSize is the size of the original data before encoding
	OriginalSize int64

	// OriginalChecksum is the MD5 of the original data
	OriginalChecksum string
}

// Encode encodes data using Reed-Solomon erasure coding
func (e *Encoder) Encode(reader io.Reader) (*EncodedData, error) {
	// Read all data into memory
	// For very large files, we should use streaming, but for now we buffer
	var buf bytes.Buffer
	originalHash := md5.New()
	writer := io.MultiWriter(&buf, originalHash)

	n, err := io.Copy(writer, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read data: %w", err)
	}

	data := buf.Bytes()
	originalChecksum := hex.EncodeToString(originalHash.Sum(nil))

	// Calculate shard size for this data
	// Each shard should be the same size, padded if necessary
	shardSize := (len(data) + e.dataShards - 1) / e.dataShards

	// Create data shards
	shards := make([][]byte, e.dataShards+e.parityShards)
	for i := 0; i < e.dataShards; i++ {
		start := i * shardSize
		end := start + shardSize
		if end > len(data) {
			end = len(data)
		}

		// Create shard with padding if needed
		shards[i] = make([]byte, shardSize)
		if start < len(data) {
			copy(shards[i], data[start:end])
		}
	}

	// Create empty parity shards
	for i := e.dataShards; i < e.dataShards+e.parityShards; i++ {
		shards[i] = make([]byte, shardSize)
	}

	// Encode parity shards
	if err := e.encoder.Encode(shards); err != nil {
		return nil, fmt.Errorf("failed to encode parity: %w", err)
	}

	// Calculate checksums for each shard
	checksums := make([]string, len(shards))
	for i, shard := range shards {
		hash := md5.Sum(shard)
		checksums[i] = hex.EncodeToString(hash[:])
	}

	return &EncodedData{
		Shards:           shards,
		ShardChecksums:   checksums,
		OriginalSize:     n,
		OriginalChecksum: originalChecksum,
	}, nil
}

// EncodeStream encodes data in chunks for streaming large files
func (e *Encoder) EncodeStream(reader io.Reader, chunkSize int) (chan *EncodedData, chan error) {
	dataChan := make(chan *EncodedData)
	errChan := make(chan error, 1)

	go func() {
		defer close(dataChan)
		defer close(errChan)

		buf := make([]byte, chunkSize)
		for {
			n, err := io.ReadFull(reader, buf)
			if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
				errChan <- fmt.Errorf("failed to read chunk: %w", err)
				return
			}

			if n == 0 {
				return
			}

			// Encode this chunk
			encoded, encErr := e.Encode(bytes.NewReader(buf[:n]))
			if encErr != nil {
				errChan <- fmt.Errorf("failed to encode chunk: %w", encErr)
				return
			}

			dataChan <- encoded

			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return
			}
		}
	}()

	return dataChan, errChan
}

// VerifyShard verifies a shard against its expected checksum
func VerifyShard(shard []byte, expectedChecksum string) bool {
	hash := md5.Sum(shard)
	return hex.EncodeToString(hash[:]) == expectedChecksum
}

// ShardIndex returns true if the index is a data shard (vs parity)
func (e *Encoder) IsDataShard(index int) bool {
	return index < e.dataShards
}

// TotalShards returns the total number of shards
func (e *Encoder) TotalShards() int {
	return e.dataShards + e.parityShards
}
