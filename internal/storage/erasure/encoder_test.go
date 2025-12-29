package erasure

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

// TestEncodeStream_ContextCancellation tests that EncodeStream goroutine terminates when context is cancelled
func TestEncodeStream_ContextCancellation(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}

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
		t.Fatalf("Unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for first chunk")
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
						if err != context.Canceled {
							t.Logf("Got error (expected context.Canceled): %v", err)
						}
					}
				case <-time.After(100 * time.Millisecond):
					t.Fatal("errChan not closed after dataChan closed")
				}
			}
		case <-timeout:
			t.Fatal("Goroutine did not terminate within 2 seconds after context cancellation")
		}
	}
}

// TestEncodeStream_FullStream tests that EncodeStream completes successfully
func TestEncodeStream_FullStream(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}

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
					if err != nil {
						t.Fatalf("Encoding error: %v", err)
					}
				default:
				}
				// Success
				if chunkCount == 0 {
					t.Fatal("Expected at least one chunk")
				}
				return
			}
			if data == nil {
				t.Fatal("Received nil data")
			}
			chunkCount++

		case err := <-errChan:
			if err != nil {
				t.Fatalf("Encoding error: %v", err)
			}

		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for encoding to complete")
		}
	}
}

// TestEncodeStream_EmptyReader tests handling of empty input
func TestEncodeStream_EmptyReader(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}

	reader := bytes.NewReader([]byte{})
	ctx := context.Background()

	dataChan, errChan := encoder.EncodeStream(ctx, reader, 4096)

	// Should close channels immediately with no data
	timeout := time.After(1 * time.Second)
	select {
	case _, ok := <-dataChan:
		if ok {
			t.Fatal("Expected dataChan to be closed for empty reader")
		}
	case <-timeout:
		t.Fatal("Timeout waiting for channels to close")
	}

	// Check no errors
	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	default:
	}
}

// TestEncodeStream_ContextCancelledBeforeRead tests cancellation before any data is read
func TestEncodeStream_ContextCancelledBeforeRead(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}

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
		if err != context.Canceled {
			t.Fatalf("Expected context.Canceled, got: %v", err)
		}
	case <-timeout:
		t.Fatal("Timeout waiting for context cancellation error")
	}

	// Channels should close
	select {
	case _, ok := <-dataChan:
		if ok {
			t.Fatal("Expected dataChan to be closed")
		}
	case <-timeout:
		t.Fatal("Timeout waiting for dataChan to close")
	}
}

// TestEncodeStream_SlowReader tests that cancellation works even with slow data source
func TestEncodeStream_SlowReader(t *testing.T) {
	encoder, err := NewEncoder(4, 2, 1024)
	if err != nil {
		t.Fatalf("Failed to create encoder: %v", err)
	}

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
		if err != nil && err != context.Canceled && err != io.EOF {
			t.Logf("Got error: %v", err)
		}
	case <-timeout:
		t.Fatal("Goroutine did not terminate within 2 seconds")
	}

	// Drain channels
	for range dataChan {
	}
}

// slowReader simulates a slow data source
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
