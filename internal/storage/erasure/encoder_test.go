package erasure

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeStream_ContextCancellation tests that EncodeStream goroutine terminates when context is cancelled.
func TestEncodeStream_ContextCancellation(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	require.NoError(t, err, "Failed to create encoder")

	// Create a large data source that would take time to read
	largeData := bytes.Repeat([]byte("test data for streaming encoding\n"), 100000)
	reader := bytes.NewReader(largeData)

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// Start encoding
	dataChan, errChan := encoder.EncodeStream(ctx, reader, 4096)

	// Read just one chunk to start the goroutine
	select {
	case <-dataChan:
		// Got first chunk
	case err := <-errChan:
		require.Fail(t, "Unexpected error", err)
	case <-time.After(5 * time.Second):
		require.Fail(t, "Timeout waiting for first chunk")
	}

	// Cancel context
	cancel()

	// Goroutine should terminate and close channels within reasonable time
	timeout := time.After(2 * time.Second)
	channelsClosed := false

	for !channelsClosed {
		select {
		case _, ok := <-dataChan:
			if !ok {
				// dataChan closed, check errChan
				select {
				case err, ok := <-errChan:
					if !ok {
						// Both channels closed
						channelsClosed = true
					} else if err != nil {
						// Should be context.Canceled
						require.ErrorIs(t, err, context.Canceled, "Expected context.Canceled error")
					}
				case <-time.After(100 * time.Millisecond):
					require.Fail(t, "errChan not closed after dataChan closed")
				}
			}
		case <-timeout:
			require.Fail(t, "Goroutine did not terminate within 2 seconds after context cancellation")
		}
	}
}

// TestEncodeStream_FullStream tests that EncodeStream completes successfully.
func TestEncodeStream_FullStream(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	require.NoError(t, err, "Failed to create encoder")

	// Create test data
	testData := bytes.Repeat([]byte("test data\n"), 1000)
	reader := bytes.NewReader(testData)

	ctx := context.Background()

	// Start encoding
	dataChan, errChan := encoder.EncodeStream(ctx, reader, 4096)

	// Read all chunks
	chunkCount := 0

	for {
		select {
		case data, ok := <-dataChan:
			if !ok {
				// Channel closed, check for errors
				select {
				case err := <-errChan:
					require.NoError(t, err, "Encoding should complete without error")
				default:
				}
				// Success
				assert.Positive(t, chunkCount, "Expected at least one chunk")

				return
			}

			assert.NotNil(t, data, "Received data should not be nil")

			chunkCount++

		case err := <-errChan:
			require.NoError(t, err, "Should not receive error during encoding")

		case <-time.After(5 * time.Second):
			require.Fail(t, "Timeout waiting for encoding to complete")
		}
	}
}

// TestEncodeStream_EmptyReader tests handling of empty input.
func TestEncodeStream_EmptyReader(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	require.NoError(t, err, "Failed to create encoder")

	reader := bytes.NewReader([]byte{})
	ctx := context.Background()

	dataChan, errChan := encoder.EncodeStream(ctx, reader, 4096)

	// Should close channels immediately with no data
	timeout := time.After(1 * time.Second)
	select {
	case _, ok := <-dataChan:
		assert.False(t, ok, "Expected dataChan to be closed for empty reader")
	case <-timeout:
		require.Fail(t, "Timeout waiting for channels to close")
	}

	// Check no errors
	select {
	case err := <-errChan:
		assert.NoError(t, err, "Should not have errors for empty reader")
	default:
	}
}

// TestEncodeStream_ContextCancelledBeforeRead tests cancellation before any data is read.
func TestEncodeStream_ContextCancelledBeforeRead(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	require.NoError(t, err, "Failed to create encoder")

	testData := bytes.Repeat([]byte("test\n"), 1000)
	reader := bytes.NewReader(testData)

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dataChan, errChan := encoder.EncodeStream(ctx, reader, 4096)

	// Should get context.Canceled error quickly
	timeout := time.After(1 * time.Second)
	select {
	case err := <-errChan:
		require.ErrorIs(t, err, context.Canceled, "Expected context.Canceled error")
	case <-timeout:
		require.Fail(t, "Timeout waiting for context cancellation error")
	}

	// Channels should close
	select {
	case _, ok := <-dataChan:
		assert.False(t, ok, "Expected dataChan to be closed")
	case <-timeout:
		require.Fail(t, "Timeout waiting for dataChan to close")
	}
}

// TestEncodeStream_SlowReader tests that cancellation works even with slow data source.
func TestEncodeStream_SlowReader(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	require.NoError(t, err, "Failed to create encoder")

	// Create a slow reader that blocks
	slowReader := &slowReader{data: bytes.Repeat([]byte("data\n"), 10000), delay: 100 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())

	dataChan, errChan := encoder.EncodeStream(ctx, slowReader, 4096)

	// Let it start reading
	time.Sleep(50 * time.Millisecond)

	// Cancel while it's reading
	cancel()

	// Should terminate quickly despite slow reader
	timeout := time.After(2 * time.Second)
	select {
	case err := <-errChan:
		// Should get context.Canceled
		if err != nil {
			require.ErrorIs(t, err, context.Canceled, "Expected context.Canceled error from slow reader")
		}
	case <-timeout:
		require.Fail(t, "Goroutine did not terminate within 2 seconds")
	}

	// Drain channels
	for range dataChan {
	}
}

// TestEncodeStream_ReadError tests handling of read errors.
func TestEncodeStream_ReadError(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	require.NoError(t, err, "Failed to create encoder")

	// Reader that returns error
	expectedErr := errors.New("disk failure")
	errReader := &errorReader{err: expectedErr}

	dataChan, errChan := encoder.EncodeStream(context.Background(), errReader, 4096)

	timeout := time.After(1 * time.Second)
	select {
	case err := <-errChan:
		require.Error(t, err, "Should receive error from failing reader")
		assert.Contains(t, err.Error(), "failed to read chunk", "Error should indicate read failure")
		require.ErrorIs(t, err, expectedErr, "Should wrap original error")
	case <-dataChan:
		require.Fail(t, "Should not receive data from failing reader")
	case <-timeout:
		require.Fail(t, "Timeout waiting for error")
	}
}

// slowReader simulates a slow data source.
type slowReader struct {
	data  []byte
	pos   int
	delay time.Duration
}

func (s *slowReader) Read(p []byte) (n int, err error) {
	if s.pos >= len(s.data) {
		return 0, io.EOF
	}

	time.Sleep(s.delay)

	n = copy(p, s.data[s.pos:])
	s.pos += n

	if s.pos >= len(s.data) {
		return n, io.EOF
	}

	return n, nil
}

// errorReader simulates a failing reader.
type errorReader struct {
	err error
}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, e.err
}
