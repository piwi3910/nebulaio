package rdma

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Transport != TransportAuto {
		t.Errorf("Expected Transport %s, got %s", TransportAuto, cfg.Transport)
	}

	if cfg.ConnectionType != ConnTypeRC {
		t.Errorf("Expected ConnectionType %s, got %s", ConnTypeRC, cfg.ConnectionType)
	}

	if cfg.SendQueueDepth != 128 {
		t.Errorf("Expected SendQueueDepth 128, got %d", cfg.SendQueueDepth)
	}

	if cfg.MemoryPoolSize != 1<<30 {
		t.Errorf("Expected MemoryPoolSize 1GB, got %d", cfg.MemoryPoolSize)
	}
}

func TestNewTransportSimulated(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	if !transport.IsAvailable() {
		t.Error("Transport should be available in simulated mode")
	}
}

func TestNewTransportNoHardware(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FallbackHTTP = false

	// Without actual hardware, this should fail
	transport, err := NewTransport(cfg)
	if err == nil && transport.IsAvailable() {
		t.Skip("RDMA hardware available, skipping no-hardware test")
	}
}

func TestNewTransportWithFallback(t *testing.T) {
	cfg := DefaultConfig()
	cfg.FallbackHTTP = true

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport with fallback failed: %v", err)
	}
	defer transport.Close()

	// Should succeed even without hardware due to fallback
}

func TestMemoryPool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated
	cfg.PreAllocateRegions = 5
	cfg.MemoryRegionSize = 1024

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	pool := transport.memPool
	if pool == nil {
		t.Fatal("Memory pool should be initialized")
	}

	ctx := context.Background()

	// Get all pre-allocated regions
	regions := make([]*MemoryRegion, 5)

	for i := range 5 {
		mr, err := pool.GetRegion(ctx)
		if err != nil {
			t.Fatalf("GetRegion failed: %v", err)
		}

		regions[i] = mr

		if len(mr.Buffer) != 1024 {
			t.Errorf("Expected buffer size 1024, got %d", len(mr.Buffer))
		}
	}

	// Release regions
	for _, mr := range regions {
		pool.ReleaseRegion(mr)
	}

	// Should be able to get again
	mr, err := pool.GetRegion(ctx)
	if err != nil {
		t.Fatalf("GetRegion after release failed: %v", err)
	}

	pool.ReleaseRegion(mr)
}

func TestMemoryPoolTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated
	cfg.PreAllocateRegions = 1
	cfg.MemoryRegionSize = 1024

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	pool := transport.memPool

	ctx := context.Background()

	// Get the only region
	mr, _ := pool.GetRegion(ctx)

	// Try to get another with timeout
	ctx2, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err = pool.GetRegion(ctx2)
	if err != context.DeadlineExceeded {
		t.Errorf("Expected DeadlineExceeded, got %v", err)
	}

	// Release and verify we can get again
	pool.ReleaseRegion(mr)

	mr2, err := pool.GetRegion(context.Background())
	if err != nil {
		t.Fatalf("GetRegion after release failed: %v", err)
	}

	pool.ReleaseRegion(mr2)
}

func TestDeviceSelection(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	tests := []struct {
		name      string
		transport string
		expected  string
		devices   []*Device
	}{
		{
			name:      "prefer InfiniBand",
			transport: TransportAuto,
			devices: []*Device{
				{Name: "roce0", Transport: TransportRoCEv2},
				{Name: "ib0", Transport: TransportInfiniBand},
				{Name: "iwarp0", Transport: TransportIWARP},
			},
			expected: "ib0",
		},
		{
			name:      "prefer RoCE",
			transport: TransportAuto,
			devices: []*Device{
				{Name: "roce0", Transport: TransportRoCEv2},
				{Name: "iwarp0", Transport: TransportIWARP},
			},
			expected: "roce0",
		},
		{
			name:      "specific transport",
			transport: TransportIWARP,
			devices: []*Device{
				{Name: "roce0", Transport: TransportRoCEv2},
				{Name: "iwarp0", Transport: TransportIWARP},
			},
			expected: "iwarp0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport.config.Transport = tt.transport

			selected := transport.selectDevice(tt.devices)
			if selected == nil {
				t.Fatal("No device selected")
			}

			if selected.Name != tt.expected {
				t.Errorf("Expected device %s, got %s", tt.expected, selected.Name)
			}
		})
	}
}

func TestConnection(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()

	// Connect
	conn, err := transport.Connect(ctx, "127.0.0.1:9100")
	if err != nil {
		t.Fatalf("Connect failed: %v", err)
	}

	if !conn.Connected {
		t.Error("Connection should be connected")
	}

	if conn.QueuePair == nil {
		t.Error("QueuePair should be initialized")
	}

	if conn.SendMR == nil || conn.RecvMR == nil {
		t.Error("Memory regions should be allocated")
	}

	// Disconnect
	err = transport.Disconnect("127.0.0.1:9100")
	if err != nil {
		t.Fatalf("Disconnect failed: %v", err)
	}

	if conn.Connected {
		t.Error("Connection should be disconnected")
	}
}

func TestReconnect(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	addr := "127.0.0.1:9100"

	// First connection
	conn1, _ := transport.Connect(ctx, addr)
	if conn1 == nil {
		t.Fatal("First connection failed")
	}

	// Second connection to same address should reuse
	conn2, _ := transport.Connect(ctx, addr)
	if conn1 != conn2 {
		t.Error("Should reuse existing connection")
	}

	// Disconnect and reconnect
	transport.Disconnect(addr)

	conn3, _ := transport.Connect(ctx, addr)
	if conn3 == nil {
		t.Fatal("Reconnection failed")
	}
}

func TestMetrics(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	transport.Connect(ctx, "127.0.0.1:9100")

	metrics := transport.GetMetrics()

	if metrics.ConnectionsTotal != 1 {
		t.Errorf("Expected 1 connection, got %d", metrics.ConnectionsTotal)
	}

	if metrics.ConnectionsActive != 1 {
		t.Errorf("Expected 1 active connection, got %d", metrics.ConnectionsActive)
	}

	if metrics.MemoryTotal != cfg.MemoryPoolSize {
		t.Errorf("Expected MemoryTotal %d, got %d", cfg.MemoryPoolSize, metrics.MemoryTotal)
	}
}

func TestListener(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	listener, err := transport.Listen(9100)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}

	if listener == nil {
		t.Fatal("Listener should not be nil")
	}

	if listener.port != 9100 {
		t.Errorf("Expected port 9100, got %d", listener.port)
	}

	listener.Close()
}

func TestSerializeRequest(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	conn, _ := transport.Connect(ctx, "127.0.0.1:9100")

	body := strings.NewReader("test data")
	req := &S3Request{
		OpCode:   OpGetObject,
		Bucket:   "my-bucket",
		Key:      "my-key",
		Body:     body,
		BodySize: 9,
	}

	data, err := conn.serializeRequest(req)
	if err != nil {
		t.Fatalf("serializeRequest failed: %v", err)
	}

	// Verify format:
	// [OpCode:1][BucketLen:2][KeyLen:2][BodyLen:8][Bucket][Key][Body]
	expectedMinLen := 1 + 2 + 2 + 8 + len("my-bucket") + len("my-key") + 9
	if len(data) != expectedMinLen {
		t.Errorf("Expected data length %d, got %d", expectedMinLen, len(data))
	}

	if data[0] != OpGetObject {
		t.Errorf("Expected OpCode %d, got %d", OpGetObject, data[0])
	}
}

func TestDeserializeResponse(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	conn, _ := transport.Connect(ctx, "127.0.0.1:9100")

	// Create response: [StatusCode:2][BodyLen:8][Body]
	bodyData := []byte("response body")
	data := make([]byte, 2+8+len(bodyData))
	data[0] = 0   // Status high byte
	data[1] = 200 // Status low byte = 200
	// Body length (8 bytes big endian)
	bodyLen := int64(len(bodyData))
	for i := 7; i >= 0; i-- {
		data[2+i] = byte(bodyLen >> (8 * (7 - i)))
	}

	copy(data[10:], bodyData)

	resp, err := conn.deserializeResponse(data)
	if err != nil {
		t.Fatalf("deserializeResponse failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	if resp.BodySize != int64(len(bodyData)) {
		t.Errorf("Expected body size %d, got %d", len(bodyData), resp.BodySize)
	}
}

func TestS3Operations(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	conn, _ := transport.Connect(ctx, "127.0.0.1:9100")

	ops := NewS3Operations(transport, conn)
	if ops == nil {
		t.Fatal("S3Operations should not be nil")
	}

	// Note: Actual operations would require a running server
	// These tests verify the operation structs are properly constructed
}

func TestClient(t *testing.T) {
	cfg := DefaultClientConfig()
	cfg.RDMAConfig.DeviceName = deviceSimulated
	cfg.FallbackHTTP = true
	cfg.HTTPEndpoint = "http://localhost:9000"

	client, err := NewClient([]string{"rdma://localhost:9100"}, cfg)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	if !client.IsRDMAEnabled() {
		t.Skip("RDMA not enabled, skipping RDMA-specific tests")
	}

	transportType := client.GetTransportType()
	if transportType != "rdma" && transportType != "http" {
		t.Errorf("Unexpected transport type: %s", transportType)
	}
}

func TestClientMetrics(t *testing.T) {
	cfg := DefaultClientConfig()
	cfg.RDMAConfig.DeviceName = deviceSimulated

	client, err := NewClient([]string{"rdma://localhost:9100"}, cfg)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	metrics := client.GetMetrics()
	if metrics == nil {
		t.Fatal("Metrics should not be nil")
	}

	if metrics.Transport != "rdma" && metrics.Transport != "http" {
		t.Errorf("Unexpected transport: %s", metrics.Transport)
	}
}

func TestAllocateBuffer(t *testing.T) {
	cfg := DefaultClientConfig()
	cfg.RDMAConfig.DeviceName = deviceSimulated

	client, err := NewClient([]string{"rdma://localhost:9100"}, cfg)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	buf, err := client.AllocateBuffer(ctx, 4096)
	if err != nil {
		t.Fatalf("AllocateBuffer failed: %v", err)
	}

	if buf == nil {
		t.Fatal("Buffer should not be nil")
	}

	client.ReleaseBuffer(buf)
}

func TestBytesReaderAt(t *testing.T) {
	data := []byte("hello world")
	r := &bytesReaderAt{data: data}

	buf := make([]byte, 5)

	n, err := r.ReadAt(buf, 0)
	if err != nil {
		t.Fatalf("ReadAt failed: %v", err)
	}

	if n != 5 {
		t.Errorf("Expected to read 5 bytes, got %d", n)
	}

	if string(buf) != "hello" {
		t.Errorf("Expected 'hello', got '%s'", string(buf))
	}

	// Read from offset
	n, err = r.ReadAt(buf, 6)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt with offset failed: %v", err)
	}

	if string(buf[:n]) != "world" {
		t.Errorf("Expected 'world', got '%s'", string(buf[:n]))
	}

	// Read past end
	_, err = r.ReadAt(buf, int64(len(data)+1))
	if err != io.EOF {
		t.Errorf("Expected EOF, got %v", err)
	}
}

func TestZeroCopyOperations(t *testing.T) {
	cfg := DefaultClientConfig()
	cfg.RDMAConfig.DeviceName = deviceSimulated

	client, err := NewClient([]string{"rdma://localhost:9100"}, cfg)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	defer client.Close()

	if !client.IsRDMAEnabled() {
		t.Skip("RDMA not enabled")
	}

	ctx := context.Background()

	// Allocate buffer for zero-copy
	buf, err := client.AllocateBuffer(ctx, 1<<20) // 1MB
	if err != nil {
		t.Fatalf("AllocateBuffer failed: %v", err)
	}
	defer client.ReleaseBuffer(buf)

	// Note: Actual zero-copy operations require running server
	// This test verifies the API is callable
}

func TestStreamGetObject(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DeviceName = deviceSimulated

	transport, err := NewTransport(cfg)
	if err != nil {
		t.Fatalf("NewTransport failed: %v", err)
	}
	defer transport.Close()

	ctx := context.Background()
	conn, _ := transport.Connect(ctx, "127.0.0.1:9100")

	ops := NewS3Operations(transport, conn)

	// Note: Would require server to test actual streaming
	_ = ops
}

func TestParseListResponse(t *testing.T) {
	// Build test data:
	// [Count:4][IsTruncated:1][NextTokenLen:2][NextToken][Objects...]
	var buf bytes.Buffer

	// Count = 2
	buf.Write([]byte{0, 0, 0, 2})

	// IsTruncated = 1 (true)
	buf.WriteByte(1)

	// NextToken = "token123"
	token := []byte("token123")
	buf.Write([]byte{0, byte(len(token))})
	buf.Write(token)

	// Object 1: key="file1.txt", size=100, etag="abc", timestamp=1000
	key1 := []byte("file1.txt")
	buf.Write([]byte{0, byte(len(key1))})
	buf.Write(key1)
	buf.Write([]byte{0, 0, 0, 0, 0, 0, 0, 100}) // size
	etag1 := []byte("abc")
	buf.Write([]byte{0, byte(len(etag1))})
	buf.Write(etag1)
	buf.Write([]byte{0, 0, 0, 0, 0, 0, 3, 232}) // timestamp = 1000

	// Object 2: key="file2.txt", size=200, etag="def", timestamp=2000
	key2 := []byte("file2.txt")
	buf.Write([]byte{0, byte(len(key2))})
	buf.Write(key2)
	buf.Write([]byte{0, 0, 0, 0, 0, 0, 0, 200}) // size
	etag2 := []byte("def")
	buf.Write([]byte{0, byte(len(etag2))})
	buf.Write(etag2)
	buf.Write([]byte{0, 0, 0, 0, 0, 0, 7, 208}) // timestamp = 2000

	objects, _, nextToken, isTruncated := parseListResponse(buf.Bytes())

	if len(objects) != 2 {
		t.Errorf("Expected 2 objects, got %d", len(objects))
	}

	if !isTruncated {
		t.Error("Expected isTruncated to be true")
	}

	if nextToken != "token123" {
		t.Errorf("Expected nextToken 'token123', got '%s'", nextToken)
	}

	if objects[0].Key != "file1.txt" {
		t.Errorf("Expected first key 'file1.txt', got '%s'", objects[0].Key)
	}

	if objects[0].Size != 100 {
		t.Errorf("Expected first size 100, got %d", objects[0].Size)
	}

	if objects[1].Key != "file2.txt" {
		t.Errorf("Expected second key 'file2.txt', got '%s'", objects[1].Key)
	}
}
