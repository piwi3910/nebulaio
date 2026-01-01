package erasure

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // G501: MD5 required for S3 ETag compatibility
	"encoding/hex"
	"fmt"
	"io"

	"github.com/klauspost/reedsolomon"
)

// Encoder handles Reed-Solomon encoding operations.
type Encoder struct {
	encoder      reedsolomon.Encoder
	dataShards   int
	parityShards int
	shardSize    int
}

// NewEncoder creates a new erasure encoder.
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

// EncodedData represents the result of encoding an object.
type EncodedData struct {
	OriginalChecksum string
	Shards           [][]byte
	ShardChecksums   []string
	OriginalSize     int64
}

// Encode encodes data using Reed-Solomon erasure coding.
func (e *Encoder) Encode(reader io.Reader) (*EncodedData, error) {
	// Read all data into memory
	// For very large files, we should use streaming, but for now we buffer
	var buf bytes.Buffer

	originalHash := md5.New() //nolint:gosec // G401: MD5 required for S3 ETag compatibility
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
	for i := range e.dataShards {
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
	err = e.encoder.Encode(shards)
	if err != nil {
		return nil, fmt.Errorf("failed to encode parity: %w", err)
	}

	// Calculate checksums for each shard
	checksums := make([]string, len(shards))
	for i, shard := range shards {
		hash := md5.Sum(shard) //nolint:gosec // G401: MD5 required for S3 ETag compatibility
		checksums[i] = hex.EncodeToString(hash[:])
	}

	return &EncodedData{
		Shards:           shards,
		ShardChecksums:   checksums,
		OriginalSize:     n,
		OriginalChecksum: originalChecksum,
	}, nil
}

// contextReader wraps an io.Reader to make it context-aware
// It checks for context cancellation before each Read operation.
type contextReader struct {
	ctx context.Context //nolint:containedctx // Wrapper pattern - context passed from caller for Read cancellation
	r   io.Reader
}

func (cr *contextReader) Read(p []byte) (n int, err error) {
	// Check if context is already cancelled before attempting read
	err = cr.ctx.Err()
	if err != nil {
		return 0, err
	}

	return cr.r.Read(p)
}

// EncodeStream encodes data in chunks for streaming large files
// The goroutine will terminate when:
//   - All data has been read and encoded
//   - An error occurs during reading or encoding
//   - The provided context is cancelled (including during blocking reads)
//
// Callers should always cancel the context or read all data from the channels
// to prevent goroutine leaks.
//
// Note: Context cancellation is checked before each read operation. If the underlying
// reader blocks indefinitely, cancellation will occur before the next read attempt.
func (e *Encoder) EncodeStream(ctx context.Context, reader io.Reader, chunkSize int) (chan *EncodedData, chan error) {
	dataChan := make(chan *EncodedData)
	errChan := make(chan error, 1)

	go func() {
		defer close(dataChan)
		defer close(errChan)

		// Wrap reader with context awareness
		ctxReader := &contextReader{ctx: ctx, r: reader}

		buf := make([]byte, chunkSize)
		for {
			// Context cancellation is now checked within io.ReadFull via contextReader
			n, err := io.ReadFull(ctxReader, buf)
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

			// Send encoded data with context cancellation check
			// to prevent blocking if receiver stops reading
			select {
			case dataChan <- encoded:
				// Successfully sent
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}

			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return
			}
		}
	}()

	return dataChan, errChan
}

// VerifyShard verifies a shard against its expected checksum.
func VerifyShard(shard []byte, expectedChecksum string) bool {
	hash := md5.Sum(shard) //nolint:gosec // G401: MD5 required for S3 ETag compatibility
	return hex.EncodeToString(hash[:]) == expectedChecksum
}

// ShardIndex returns true if the index is a data shard (vs parity).
func (e *Encoder) IsDataShard(index int) bool {
	return index < e.dataShards
}

// TotalShards returns the total number of shards.
func (e *Encoder) TotalShards() int {
	return e.dataShards + e.parityShards
}
