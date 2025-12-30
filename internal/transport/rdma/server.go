package rdma

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// Server provides an RDMA-enabled S3 server.
type Server struct {
	handler   RequestHandler
	config    *ServerConfig
	transport *Transport
	listener  *Listener
	metrics   *ServerMetrics
	wg        sync.WaitGroup
	running   atomic.Bool
}

// ServerConfig configures the RDMA server.
type ServerConfig struct {
	RDMAConfig      *Config
	ListenAddr      string
	Port            int
	WorkerCount     int
	MaxRequestSize  int64
	MaxResponseSize int64
	RequestTimeout  time.Duration
}

// DefaultServerConfig returns default server configuration.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Port:            9100,
		RDMAConfig:      DefaultConfig(),
		WorkerCount:     16,
		MaxRequestSize:  1 << 30, // 1GB
		MaxResponseSize: 1 << 30,
		RequestTimeout:  60 * time.Second,
	}
}

// RequestHandler handles S3 requests over RDMA.
type RequestHandler interface {
	// HandleRequest processes an S3 request and returns the response
	HandleRequest(ctx context.Context, req *ServerRequest) (*ServerResponse, error)
}

// ServerRequest represents an incoming S3 request.
type ServerRequest struct {
	Body       io.Reader
	Headers    map[string]string
	Connection *Connection
	Bucket     string
	Key        string
	VersionID  string
	RemoteAddr string
	BodySize   int64
	OpCode     uint8
}

// ServerResponse represents the response to send.
type ServerResponse struct {
	Body        io.Reader
	Headers     map[string]string
	ContentType string
	ETag        string
	StatusCode  int
	BodySize    int64
}

// ServerMetrics tracks server performance metrics.
type ServerMetrics struct {
	RequestsByOp      map[uint8]int64
	RequestsTotal     int64
	RequestsSuccess   int64
	RequestsFailed    int64
	BytesReceived     int64
	BytesSent         int64
	ActiveConnections int64
	LatencySum        time.Duration
	LatencyCount      int64
	mu                sync.RWMutex
}

// NewServer creates a new RDMA server.
func NewServer(config *ServerConfig, handler RequestHandler) (*Server, error) {
	if config == nil {
		config = DefaultServerConfig()
	}

	if handler == nil {
		return nil, errors.New("request handler required")
	}

	transport, err := NewTransport(config.RDMAConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create RDMA transport: %w", err)
	}

	return &Server{
		config:    config,
		transport: transport,
		handler:   handler,
		metrics: &ServerMetrics{
			RequestsByOp: make(map[uint8]int64),
		},
	}, nil
}

// Start starts the RDMA server.
func (s *Server) Start() error {
	if !s.transport.IsAvailable() {
		return ErrRDMANotAvailable
	}

	listener, err := s.transport.Listen(s.config.Port)
	if err != nil {
		return fmt.Errorf("failed to start listener: %w", err)
	}

	s.listener = listener

	s.running.Store(true)

	// Start accept loop
	s.wg.Add(1)

	go s.acceptLoop()

	return nil
}

// acceptLoop accepts incoming connections.
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for s.running.Load() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, err := s.listener.Accept(ctx)

		cancel()

		if err != nil {
			if s.running.Load() {
				// Log error but continue accepting
				continue
			}

			return
		}

		atomic.AddInt64(&s.metrics.ActiveConnections, 1)

		// Handle connection in goroutine
		s.wg.Add(1)

		go s.handleConnection(conn)
	}
}

// handleConnection handles a single RDMA connection.
func (s *Server) handleConnection(conn *Connection) {
	defer s.wg.Done()
	defer func() {
		atomic.AddInt64(&s.metrics.ActiveConnections, -1)
		_ = s.transport.Disconnect(conn.RemoteAddr)
	}()

	for s.running.Load() && conn.Connected {
		err := s.handleRequest(conn)
		if err != nil {
			if err == io.EOF || !conn.Connected {
				return
			}
			// Log error but continue
		}
	}
}

// handleRequest handles a single request on a connection.
func (s *Server) handleRequest(conn *Connection) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.config.RequestTimeout)
	defer cancel()

	start := time.Now()

	// Receive request
	conn.mu.Lock()

	// Post receive
	err := conn.postReceive(ctx)
	if err != nil {
		conn.mu.Unlock()
		return err
	}

	// Wait for request
	wc, err := conn.waitReceive(ctx)
	if err != nil {
		conn.mu.Unlock()
		return err
	}

	if wc.ByteLen == 0 {
		conn.mu.Unlock()
		return nil // No data received yet (simulated mode)
	}

	// Parse request from receive buffer
	req, err := s.parseRequest(conn.RecvMR.Buffer[:wc.ByteLen], conn)
	conn.mu.Unlock()

	if err != nil {
		atomic.AddInt64(&s.metrics.RequestsFailed, 1)
		return s.sendErrorResponse(conn, 400, err.Error())
	}

	atomic.AddInt64(&s.metrics.RequestsTotal, 1)
	atomic.AddInt64(&s.metrics.BytesReceived, int64(wc.ByteLen))

	// Update op metrics
	s.metrics.mu.Lock()
	s.metrics.RequestsByOp[req.OpCode]++
	s.metrics.mu.Unlock()

	// Handle request
	resp, err := s.handler.HandleRequest(ctx, req)
	if err != nil {
		atomic.AddInt64(&s.metrics.RequestsFailed, 1)
		return s.sendErrorResponse(conn, 500, err.Error())
	}

	// Send response
	err = s.sendResponse(conn, resp)
	if err != nil {
		atomic.AddInt64(&s.metrics.RequestsFailed, 1)
		return err
	}

	atomic.AddInt64(&s.metrics.RequestsSuccess, 1)

	// Update latency metrics
	latency := time.Since(start)

	s.metrics.mu.Lock()
	s.metrics.LatencySum += latency
	s.metrics.LatencyCount++
	s.metrics.mu.Unlock()

	return nil
}

// parseRequest parses an incoming request from buffer.
func (s *Server) parseRequest(data []byte, conn *Connection) (*ServerRequest, error) {
	if len(data) < 5 { // OpCode + BucketLen + KeyLen minimum
		return nil, errors.New("request too short")
	}

	offset := 0

	// OpCode
	opCode := data[offset]
	offset++

	// Bucket length
	bucketLen := int(data[offset])<<8 | int(data[offset+1])
	offset += 2

	if offset+bucketLen > len(data) {
		return nil, errors.New("bucket length exceeds data")
	}

	bucket := string(data[offset : offset+bucketLen])
	offset += bucketLen

	// Key length
	if offset+2 > len(data) {
		return nil, errors.New("key length missing")
	}

	keyLen := int(data[offset])<<8 | int(data[offset+1])
	offset += 2

	if offset+keyLen > len(data) {
		return nil, errors.New("key length exceeds data")
	}

	key := string(data[offset : offset+keyLen])
	offset += keyLen

	// Body length (8 bytes)
	var bodyLen int64

	if offset+8 <= len(data) {
		for i := range 8 {
			bodyLen = bodyLen<<8 | int64(data[offset+i])
		}

		offset += 8
	}

	// Body
	var body io.Reader
	if bodyLen > 0 && offset+int(bodyLen) <= len(data) {
		body = bytes.NewReader(data[offset : offset+int(bodyLen)])
	}

	return &ServerRequest{
		OpCode:     opCode,
		Bucket:     bucket,
		Key:        key,
		Body:       body,
		BodySize:   bodyLen,
		RemoteAddr: conn.RemoteAddr,
		Connection: conn,
	}, nil
}

// sendResponse sends a response to the client.
func (s *Server) sendResponse(conn *Connection, resp *ServerResponse) error {
	conn.mu.Lock()
	defer conn.mu.Unlock()

	// Build response: [StatusCode:2][BodyLen:8][Body]
	var respData bytes.Buffer

	// Status code (2 bytes big endian)
	respData.WriteByte(byte(resp.StatusCode >> 8))
	respData.WriteByte(byte(resp.StatusCode))

	// Body length (8 bytes big endian)
	var bodyData []byte

	if resp.Body != nil && resp.BodySize > 0 {
		var err error

		bodyData, err = io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
	}

	bodyLen := int64(len(bodyData))
	for i := 7; i >= 0; i-- {
		respData.WriteByte(byte(bodyLen >> (8 * (7 - i))))
	}

	// Body data
	if len(bodyData) > 0 {
		respData.Write(bodyData)
	}

	// Copy to send buffer
	if respData.Len() > len(conn.SendMR.Buffer) {
		return ErrBufferTooSmall
	}

	copy(conn.SendMR.Buffer, respData.Bytes())

	// Post send
	ctx := context.Background()

	err := conn.postSend(ctx, respData.Len())
	if err != nil {
		return err
	}

	// Wait for completion
	err = conn.waitCompletion(ctx, conn.QueuePair.SendCQ)
	if err != nil {
		return err
	}

	atomic.AddInt64(&s.metrics.BytesSent, int64(respData.Len()))

	return nil
}

// sendErrorResponse sends an error response.
func (s *Server) sendErrorResponse(conn *Connection, statusCode int, message string) error {
	return s.sendResponse(conn, &ServerResponse{
		StatusCode:  statusCode,
		Body:        bytes.NewReader([]byte(message)),
		BodySize:    int64(len(message)),
		ContentType: "text/plain",
	})
}

// Stop stops the RDMA server.
func (s *Server) Stop() error {
	if !s.running.CompareAndSwap(true, false) {
		return nil
	}

	if s.listener != nil {
		_ = s.listener.Close()
	}

	// Wait for all goroutines to finish
	done := make(chan struct{})

	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		return errors.New("timeout waiting for connections to close")
	}

	if s.transport != nil {
		_ = s.transport.Close()
	}

	return nil
}

// GetMetrics returns server metrics.
func (s *Server) GetMetrics() *ServerMetrics {
	s.metrics.mu.RLock()
	defer s.metrics.mu.RUnlock()

	m := &ServerMetrics{
		RequestsTotal:     atomic.LoadInt64(&s.metrics.RequestsTotal),
		RequestsSuccess:   atomic.LoadInt64(&s.metrics.RequestsSuccess),
		RequestsFailed:    atomic.LoadInt64(&s.metrics.RequestsFailed),
		BytesReceived:     atomic.LoadInt64(&s.metrics.BytesReceived),
		BytesSent:         atomic.LoadInt64(&s.metrics.BytesSent),
		ActiveConnections: atomic.LoadInt64(&s.metrics.ActiveConnections),
		LatencySum:        s.metrics.LatencySum,
		LatencyCount:      s.metrics.LatencyCount,
		RequestsByOp:      make(map[uint8]int64),
	}

	for op, count := range s.metrics.RequestsByOp {
		m.RequestsByOp[op] = count
	}

	return m
}

// AverageLatency returns the average request latency.
func (m *ServerMetrics) AverageLatency() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.LatencyCount == 0 {
		return 0
	}

	return m.LatencySum / time.Duration(m.LatencyCount)
}

// S3Handler provides a default S3 request handler.
type S3Handler struct {
	// Backend storage interface
	Storage StorageBackend
}

// StorageBackend interface for object storage operations.
type StorageBackend interface {
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, map[string]string, error)
	PutObject(ctx context.Context, bucket, key string, data io.Reader, size int64, metadata map[string]string) (string, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	HeadObject(ctx context.Context, bucket, key string) (int64, map[string]string, error)
	ListObjects(ctx context.Context, bucket, prefix string, maxKeys int) ([]ObjectInfo, error)
}

// NewS3Handler creates a new S3 request handler.
func NewS3Handler(storage StorageBackend) *S3Handler {
	return &S3Handler{Storage: storage}
}

// HandleRequest processes an S3 request.
func (h *S3Handler) HandleRequest(ctx context.Context, req *ServerRequest) (*ServerResponse, error) {
	switch req.OpCode {
	case OpGetObject:
		return h.handleGetObject(ctx, req)
	case OpPutObject:
		return h.handlePutObject(ctx, req)
	case OpDeleteObject:
		return h.handleDeleteObject(ctx, req)
	case OpHeadObject:
		return h.handleHeadObject(ctx, req)
	case OpListObjects:
		return h.handleListObjects(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported operation: %d", req.OpCode)
	}
}

func (h *S3Handler) handleGetObject(ctx context.Context, req *ServerRequest) (*ServerResponse, error) {
	body, metadata, err := h.Storage.GetObject(ctx, req.Bucket, req.Key)
	if err != nil {
		return &ServerResponse{StatusCode: 404}, nil
	}

	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}

	return &ServerResponse{
		StatusCode:  200,
		ContentType: metadata["Content-Type"],
		Headers:     metadata,
		Body:        bytes.NewReader(data),
		BodySize:    int64(len(data)),
		ETag:        metadata["ETag"],
	}, nil
}

func (h *S3Handler) handlePutObject(ctx context.Context, req *ServerRequest) (*ServerResponse, error) {
	etag, err := h.Storage.PutObject(ctx, req.Bucket, req.Key, req.Body, req.BodySize, req.Headers)
	if err != nil {
		return nil, err
	}

	return &ServerResponse{
		StatusCode: 200,
		ETag:       etag,
	}, nil
}

func (h *S3Handler) handleDeleteObject(ctx context.Context, req *ServerRequest) (*ServerResponse, error) {
	err := h.Storage.DeleteObject(ctx, req.Bucket, req.Key)
	if err != nil {
		return nil, err
	}

	return &ServerResponse{
		StatusCode: 204,
	}, nil
}

func (h *S3Handler) handleHeadObject(ctx context.Context, req *ServerRequest) (*ServerResponse, error) {
	size, metadata, err := h.Storage.HeadObject(ctx, req.Bucket, req.Key)
	if err != nil {
		//nolint:nilerr // HTTP 404 response for missing object is a valid response, not an error
		return &ServerResponse{StatusCode: 404}, nil
	}

	return &ServerResponse{
		StatusCode:  200,
		ContentType: metadata["Content-Type"],
		Headers:     metadata,
		BodySize:    size,
		ETag:        metadata["ETag"],
	}, nil
}

func (h *S3Handler) handleListObjects(ctx context.Context, req *ServerRequest) (*ServerResponse, error) {
	maxKeys := 1000
	prefix := ""

	if req.Headers != nil {
		if v, ok := req.Headers["prefix"]; ok {
			prefix = v
		}
	}

	objects, err := h.Storage.ListObjects(ctx, req.Bucket, prefix, maxKeys)
	if err != nil {
		return nil, err
	}

	// Encode response
	var buf bytes.Buffer

	// Count (4 bytes)
	//nolint:gosec // G115: len(objects) bounded by maxKeys
	binary.Write(&buf, binary.BigEndian, uint32(len(objects)))

	// IsTruncated (1 byte)
	isTruncated := byte(0)
	if len(objects) >= maxKeys {
		isTruncated = 1
	}

	buf.WriteByte(isTruncated)

	// NextToken (2 bytes len + data) - empty for simplicity
	buf.Write([]byte{0, 0})

	// Objects
	for _, obj := range objects {
		keyBytes := []byte(obj.Key)
		//nolint:gosec // G115: key length bounded by S3 key limits
		binary.Write(&buf, binary.BigEndian, uint16(len(keyBytes)))
		buf.Write(keyBytes)

		binary.Write(&buf, binary.BigEndian, obj.Size)

		etagBytes := []byte(obj.ETag)
		//nolint:gosec // G115: ETag length bounded by S3 spec
		binary.Write(&buf, binary.BigEndian, uint16(len(etagBytes)))
		buf.Write(etagBytes)

		binary.Write(&buf, binary.BigEndian, obj.LastModified.Unix())
	}

	return &ServerResponse{
		StatusCode:  200,
		ContentType: "application/octet-stream",
		Body:        &buf,
		BodySize:    int64(buf.Len()),
	}, nil
}
