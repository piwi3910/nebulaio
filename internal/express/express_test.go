package express

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// MockObjectStore implements ObjectStore for testing.
type MockObjectStore struct {
	objects map[string]*mockObject
	buckets map[string]*BucketInfo
	mu      sync.RWMutex
}

type mockObject struct {
	modified time.Time
	meta     map[string]string
	data     []byte
}

func newMockStore() *MockObjectStore {
	return &MockObjectStore{
		objects: make(map[string]*mockObject),
		buckets: map[string]*BucketInfo{
			"express-bucket": {
				Name:              "express-bucket",
				CreationDate:      time.Now(),
				IsExpressBucket:   true,
				VersioningEnabled: false,
			},
			"normal-bucket": {
				Name:              "normal-bucket",
				CreationDate:      time.Now(),
				IsExpressBucket:   false,
				VersioningEnabled: false,
			},
		},
	}
}

func (m *MockObjectStore) GetObject(ctx context.Context, bucket, key string) (*Object, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	k := bucket + "/" + key

	obj, ok := m.objects[k]
	if !ok {
		return nil, ErrObjectNotFound
	}

	return &Object{
		Key:          key,
		Data:         io.NopCloser(bytes.NewReader(obj.data)),
		Size:         int64(len(obj.data)),
		ETag:         obj.meta["etag"],
		LastModified: obj.modified,
		Metadata:     obj.meta,
		IsExpress:    obj.meta["x-amz-express-object"] == "true",
		CurrentSize:  int64(len(obj.data)),
	}, nil
}

func (m *MockObjectStore) PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, meta map[string]string) (*PutResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, err := io.ReadAll(data)
	if err != nil {
		return nil, err
	}

	k := bucket + "/" + key
	m.objects[k] = &mockObject{
		data:     content,
		meta:     meta,
		modified: time.Now(),
	}

	return &PutResult{
		ETag:         meta["etag"],
		Size:         int64(len(content)),
		LastModified: time.Now(),
	}, nil
}

func (m *MockObjectStore) DeleteObject(ctx context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := bucket + "/" + key
	delete(m.objects, k)

	return nil
}

func (m *MockObjectStore) ListObjects(ctx context.Context, bucket, prefix string, opts ListOptions) (<-chan ObjectInfo, <-chan error) {
	objChan := make(chan ObjectInfo, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(objChan)
		defer close(errChan)

		m.mu.RLock()
		defer m.mu.RUnlock()

		count := 0
		for k, obj := range m.objects {
			if count >= opts.MaxKeys && opts.MaxKeys > 0 {
				break
			}

			// Check bucket prefix
			if len(k) <= len(bucket)+1 {
				continue
			}

			if k[:len(bucket)] != bucket {
				continue
			}

			key := k[len(bucket)+1:]
			if prefix != "" && len(key) < len(prefix) {
				continue
			}

			if prefix != "" && key[:len(prefix)] != prefix {
				continue
			}

			select {
			case objChan <- ObjectInfo{
				Key:          key,
				Size:         int64(len(obj.data)),
				ETag:         obj.meta["etag"],
				LastModified: obj.modified,
			}:
				count++
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}
		}
	}()

	return objChan, errChan
}

func (m *MockObjectStore) GetObjectSize(ctx context.Context, bucket, key string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	k := bucket + "/" + key

	obj, ok := m.objects[k]
	if !ok {
		return 0, ErrObjectNotFound
	}

	return int64(len(obj.data)), nil
}

func (m *MockObjectStore) AppendObject(ctx context.Context, bucket, key string, data io.Reader, size int64, offset int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k := bucket + "/" + key

	content, err := io.ReadAll(data)
	if err != nil {
		return err
	}

	obj, ok := m.objects[k]
	if !ok {
		// Create new object
		m.objects[k] = &mockObject{
			data:     content,
			meta:     map[string]string{"x-amz-express-object": "true"},
			modified: time.Now(),
		}

		return nil
	}

	// Verify offset
	if offset != int64(len(obj.data)) {
		return ErrOffsetConflict
	}

	// Append data
	obj.data = append(obj.data, content...)
	obj.modified = time.Now()

	return nil
}

func (m *MockObjectStore) HeadBucket(ctx context.Context, bucket string) (*BucketInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	info, ok := m.buckets[bucket]
	if !ok {
		return nil, errors.New("bucket not found")
	}

	return info, nil
}

func TestCreateSession(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	// Test creating session for express bucket
	session, err := svc.CreateSession(ctx, "express-bucket")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if session.Bucket != "express-bucket" {
		t.Errorf("Expected bucket 'express-bucket', got '%s'", session.Bucket)
	}

	if session.Credentials.AccessKeyID == "" {
		t.Error("Expected AccessKeyID to be set")
	}

	if session.Credentials.SessionToken == "" {
		t.Error("Expected SessionToken to be set")
	}

	// Test creating session for non-express bucket
	_, err = svc.CreateSession(ctx, "normal-bucket")
	if !errors.Is(err, ErrBucketNotExpress) {
		t.Errorf("Expected ErrBucketNotExpress, got %v", err)
	}
}

func TestValidateSession(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "express-bucket")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Valid session
	validated, err := svc.ValidateSession(session.ID)
	if err != nil {
		t.Fatalf("ValidateSession failed: %v", err)
	}

	if validated.ID != session.ID {
		t.Errorf("Session ID mismatch: expected %s, got %s", session.ID, validated.ID)
	}

	// Invalid session
	_, err = svc.ValidateSession("invalid-session-id")
	if !errors.Is(err, ErrInvalidSession) {
		t.Errorf("Expected ErrInvalidSession, got %v", err)
	}
}

func TestExpressPutObject(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	data := []byte("Hello, Express API!")

	result, err := svc.ExpressPutObject(ctx, "express-bucket", "test-key", bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("ExpressPutObject failed: %v", err)
	}

	if result.ETag == "" {
		t.Error("Expected ETag to be set")
	}

	// Verify lightweight ETag was used
	metrics := svc.GetMetrics()
	if metrics.LightweightETagsUsed != 1 {
		t.Errorf("Expected 1 lightweight ETag, got %d", metrics.LightweightETagsUsed)
	}

	// Verify object can be retrieved
	obj, err := store.GetObject(ctx, "express-bucket", "test-key")
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer obj.Data.Close()

	content, _ := io.ReadAll(obj.Data)
	if string(content) != string(data) {
		t.Errorf("Content mismatch: expected %s, got %s", data, content)
	}
}

func TestExpressAppendObject(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	// First append creates the object
	data1 := []byte("Hello, ")

	result1, err := svc.ExpressAppendObject(ctx, "express-bucket", "append-key", bytes.NewReader(data1), int64(len(data1)), -1, "writer1")
	if err != nil {
		t.Fatalf("First append failed: %v", err)
	}

	if result1.Offset != 0 {
		t.Errorf("Expected first append at offset 0, got %d", result1.Offset)
	}

	if result1.NewSize != int64(len(data1)) {
		t.Errorf("Expected new size %d, got %d", len(data1), result1.NewSize)
	}

	// Second append continues from end
	data2 := []byte("World!")

	result2, err := svc.ExpressAppendObject(ctx, "express-bucket", "append-key", bytes.NewReader(data2), int64(len(data2)), -1, "writer1")
	if err != nil {
		t.Fatalf("Second append failed: %v", err)
	}

	if result2.Offset != int64(len(data1)) {
		t.Errorf("Expected second append at offset %d, got %d", len(data1), result2.Offset)
	}

	expectedTotal := int64(len(data1) + len(data2))
	if result2.NewSize != expectedTotal {
		t.Errorf("Expected new size %d, got %d", expectedTotal, result2.NewSize)
	}

	// Verify final content
	obj, err := store.GetObject(ctx, "express-bucket", "append-key")
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer obj.Data.Close()

	content, _ := io.ReadAll(obj.Data)

	expected := "Hello, World!"
	if string(content) != expected {
		t.Errorf("Content mismatch: expected '%s', got '%s'", expected, content)
	}
}

func TestExpressAppendOffsetConflict(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	// Create initial object
	data1 := []byte("Initial data")

	_, err := svc.ExpressAppendObject(ctx, "express-bucket", "conflict-key", bytes.NewReader(data1), int64(len(data1)), -1, "writer1")
	if err != nil {
		t.Fatalf("Initial append failed: %v", err)
	}

	// Try to append at wrong offset
	data2 := []byte("More data")

	_, err = svc.ExpressAppendObject(ctx, "express-bucket", "conflict-key", bytes.NewReader(data2), int64(len(data2)), 0, "writer2")
	if err == nil {
		t.Error("Expected offset conflict error")
	}

	if !errors.Is(err, ErrOffsetConflict) {
		t.Errorf("Expected ErrOffsetConflict, got %v", err)
	}

	// Verify append conflict metric
	metrics := svc.GetMetrics()
	if metrics.AppendConflicts != 1 {
		t.Errorf("Expected 1 append conflict, got %d", metrics.AppendConflicts)
	}
}

func TestExpressListObjects(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, &Config{
		SessionDuration:        time.Hour,
		StreamingListBatchSize: 2,
		EnableAtomicAppend:     true,
		EnableLightweightETags: true,
	})

	ctx := context.Background()

	// Create test objects
	for i := range 5 {
		data := []byte("test data")

		_, err := svc.ExpressPutObject(ctx, "express-bucket", "list-test/file"+string(rune('0'+i))+".txt", bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("PutObject failed: %v", err)
		}
	}

	// List with streaming
	resultChan, errChan := svc.ExpressListObjects(ctx, "express-bucket", "list-test/", ListOptions{MaxKeys: 10})

	var allObjects []ObjectInfo
	for result := range resultChan {
		allObjects = append(allObjects, result.Objects...)
	}

	// Check for errors
	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
	default:
	}

	if len(allObjects) != 5 {
		t.Errorf("Expected 5 objects, got %d", len(allObjects))
	}
}

func TestGetWriteMarker(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	// Create object with appends
	data := []byte("Test data for marker")

	_, err := svc.ExpressAppendObject(ctx, "express-bucket", "marker-key", bytes.NewReader(data), int64(len(data)), -1, "test-writer")
	if err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	marker, err := svc.GetWriteMarker(ctx, "express-bucket", "marker-key")
	if err != nil {
		t.Fatalf("GetWriteMarker failed: %v", err)
	}

	if marker.CurrentSize != int64(len(data)) {
		t.Errorf("Expected size %d, got %d", len(data), marker.CurrentSize)
	}

	if marker.Bucket != "express-bucket" {
		t.Errorf("Expected bucket 'express-bucket', got '%s'", marker.Bucket)
	}

	if marker.Key != "marker-key" {
		t.Errorf("Expected key 'marker-key', got '%s'", marker.Key)
	}
}

func TestBatchAppend(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	requests := []BatchAppendRequest{
		{Bucket: "express-bucket", Key: "batch1", Data: []byte("First batch"), Offset: -1, WriterID: "writer1"},
		{Bucket: "express-bucket", Key: "batch2", Data: []byte("Second batch"), Offset: -1, WriterID: "writer1"},
		{Bucket: "express-bucket", Key: "batch3", Data: []byte("Third batch"), Offset: -1, WriterID: "writer1"},
	}

	result := svc.ExpressBatchAppend(ctx, requests)

	// Check all succeeded
	for i, err := range result.Errors {
		if err != nil {
			t.Errorf("Batch append %d failed: %v", i, err)
		}
	}

	// Verify objects exist
	for _, req := range requests {
		obj, err := store.GetObject(ctx, req.Bucket, req.Key)
		if err != nil {
			t.Errorf("Object %s not found: %v", req.Key, err)
			continue
		}

		obj.Data.Close()

		if obj.Size != int64(len(req.Data)) {
			t.Errorf("Object %s size mismatch: expected %d, got %d", req.Key, len(req.Data), obj.Size)
		}
	}
}

func TestLightweightETag(t *testing.T) {
	etag := generateLightweightETag()

	if etag == "" {
		t.Error("Expected non-empty ETag")
	}

	if etag[0] != '"' || etag[len(etag)-1] != '"' {
		t.Errorf("ETag should be quoted: %s", etag)
	}

	// Should be 32 hex chars + 2 quotes
	if len(etag) != 34 {
		t.Errorf("Expected ETag length 34, got %d", len(etag))
	}
}

func TestCreateChecksum(t *testing.T) {
	data := []byte("Test checksum data")
	checksum := CreateChecksum(data)

	if checksum.Algorithm != "CRC32" {
		t.Errorf("Expected algorithm CRC32, got %s", checksum.Algorithm)
	}

	if checksum.Value == "" {
		t.Error("Expected non-empty checksum value")
	}

	// Same data should produce same checksum
	checksum2 := CreateChecksum(data)
	if checksum.Value != checksum2.Value {
		t.Error("Same data should produce same checksum")
	}

	// Different data should produce different checksum
	checksum3 := CreateChecksum([]byte("Different data"))
	if checksum.Value == checksum3.Value {
		t.Error("Different data should produce different checksum")
	}
}

func TestMetrics(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	// Perform some operations
	data := []byte("Metrics test data")

	for i := range 3 {
		key := "metrics-test-" + string(rune('0'+i))

		_, err := svc.ExpressPutObject(ctx, "express-bucket", key, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("PutObject failed: %v", err)
		}
	}

	_, err := svc.CreateSession(ctx, "express-bucket")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	metrics := svc.GetMetrics()

	if metrics.PutOperations != 3 {
		t.Errorf("Expected 3 PUT operations, got %d", metrics.PutOperations)
	}

	expectedBytes := int64(len(data) * 3)
	if metrics.PutBytesWritten != expectedBytes {
		t.Errorf("Expected %d bytes written, got %d", expectedBytes, metrics.PutBytesWritten)
	}

	if metrics.SessionsCreated != 1 {
		t.Errorf("Expected 1 session created, got %d", metrics.SessionsCreated)
	}

	if metrics.LightweightETagsUsed != 3 {
		t.Errorf("Expected 3 lightweight ETags, got %d", metrics.LightweightETagsUsed)
	}
}

func TestConcurrentAppends(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	// Create initial object
	initial := []byte("Start:")

	_, err := svc.ExpressAppendObject(ctx, "express-bucket", "concurrent-key", bytes.NewReader(initial), int64(len(initial)), -1, "init")
	if err != nil {
		t.Fatalf("Initial append failed: %v", err)
	}

	// Concurrent appends - only one should succeed at each offset
	var wg sync.WaitGroup

	successCount := int64(0)
	conflictCount := int64(0)

	for i := range 5 {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			data := []byte("X")

			_, err := svc.ExpressAppendObject(ctx, "express-bucket", "concurrent-key", bytes.NewReader(data), 1, -1, "writer"+string(rune('0'+id)))
			if err == nil {
				atomic.AddInt64(&successCount, 1)
			} else if errors.Is(err, ErrOffsetConflict) {
				atomic.AddInt64(&conflictCount, 1)
			}
		}(i)
	}

	wg.Wait()

	// Due to sequential locking, all should succeed (they wait for lock)
	// but they get serialized
	t.Logf("Successes: %d, Conflicts: %d", successCount, conflictCount)
}

func TestDirectoryBucket(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	bucket, err := svc.CreateDirectoryBucket(ctx, "my-bucket", "use1-az1")
	if err != nil {
		t.Fatalf("CreateDirectoryBucket failed: %v", err)
	}

	expectedName := "my-bucket--use1-az1--x-s3"
	if bucket.Name != expectedName {
		t.Errorf("Expected bucket name '%s', got '%s'", expectedName, bucket.Name)
	}

	if bucket.AvailabilityZone != "use1-az1" {
		t.Errorf("Expected AZ 'use1-az1', got '%s'", bucket.AvailabilityZone)
	}

	if bucket.DataRedundancy != "SingleAvailabilityZone" {
		t.Errorf("Expected SingleAvailabilityZone, got '%s'", bucket.DataRedundancy)
	}
}

func TestExpressDeleteObject(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	// Create object
	data := []byte("To be deleted")

	_, err := svc.ExpressPutObject(ctx, "express-bucket", "delete-me", bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	// Delete object
	err = svc.ExpressDeleteObject(ctx, "express-bucket", "delete-me")
	if err != nil {
		t.Fatalf("DeleteObject failed: %v", err)
	}

	// Verify deleted
	_, err = store.GetObject(ctx, "express-bucket", "delete-me")
	if !errors.Is(err, ErrObjectNotFound) {
		t.Errorf("Expected ErrObjectNotFound, got %v", err)
	}
}

func TestExpressCopyObject(t *testing.T) {
	store := newMockStore()
	svc := NewExpressService(store, nil)

	ctx := context.Background()

	// Create source object
	data := []byte("Source content to copy")

	_, err := svc.ExpressPutObject(ctx, "express-bucket", "source-key", bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("PutObject failed: %v", err)
	}

	// Copy object
	result, err := svc.ExpressCopyObject(ctx, "express-bucket", "source-key", "express-bucket", "dest-key")
	if err != nil {
		t.Fatalf("CopyObject failed: %v", err)
	}

	if result.Size != int64(len(data)) {
		t.Errorf("Expected size %d, got %d", len(data), result.Size)
	}

	// Verify destination content
	obj, err := store.GetObject(ctx, "express-bucket", "dest-key")
	if err != nil {
		t.Fatalf("GetObject failed: %v", err)
	}
	defer obj.Data.Close()

	content, _ := io.ReadAll(obj.Data)
	if string(content) != string(data) {
		t.Errorf("Content mismatch: expected '%s', got '%s'", data, content)
	}
}
