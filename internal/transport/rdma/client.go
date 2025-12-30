package rdma

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Transport type names for metrics.
const (
	transportTypeRDMA = "rdma"
	transportTypeHTTP = "http"
)

// Client provides a high-level S3-compatible client with RDMA support.
type Client struct {
	config     *ClientConfig
	transport  *Transport
	httpClient *http.Client
	conns      map[string]*Connection
	ops        map[string]*S3Operations
	endpoints  []string
	mu         sync.RWMutex
	closed     bool
}

// ClientConfig configures the RDMA client.
type ClientConfig struct {
	RDMAConfig       *Config
	HTTPEndpoint     string
	AccessKey        string
	SecretKey        string
	Endpoints        []string
	MaxConnections   int
	ConnectionTTL    time.Duration
	ReconnectBackoff time.Duration
	RequestTimeout   time.Duration
	RetryAttempts    int
	RetryDelay       time.Duration
	FallbackHTTP     bool
}

// DefaultClientConfig returns a default client configuration.
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		RDMAConfig:       DefaultConfig(),
		FallbackHTTP:     true,
		MaxConnections:   10,
		ConnectionTTL:    5 * time.Minute,
		ReconnectBackoff: time.Second,
		RequestTimeout:   30 * time.Second,
		RetryAttempts:    3,
		RetryDelay:       100 * time.Millisecond,
	}
}

// NewClient creates a new RDMA-enabled S3 client.
func NewClient(endpoints []string, config *ClientConfig) (*Client, error) {
	if config == nil {
		config = DefaultClientConfig()
	}

	if len(endpoints) == 0 && len(config.Endpoints) == 0 {
		return nil, errors.New("at least one endpoint required")
	}

	if len(endpoints) == 0 {
		endpoints = config.Endpoints
	}

	// Create RDMA transport
	transport, err := NewTransport(config.RDMAConfig)
	if err != nil && !config.FallbackHTTP {
		return nil, err
	}

	client := &Client{
		config:    config,
		transport: transport,
		endpoints: endpoints,
		conns:     make(map[string]*Connection),
		ops:       make(map[string]*S3Operations),
	}

	// Set up HTTP fallback client
	if config.FallbackHTTP {
		client.httpClient = &http.Client{
			Timeout: config.RequestTimeout,
		}
	}

	return client, nil
}

// getConnection gets or creates a connection to an endpoint.
func (c *Client) getConnection(ctx context.Context, endpoint string) (*Connection, error) {
	c.mu.RLock()
	conn, exists := c.conns[endpoint]
	c.mu.RUnlock()

	if exists && conn.Connected {
		return conn, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if conn, exists = c.conns[endpoint]; exists && conn.Connected {
		return conn, nil
	}

	// Parse endpoint
	addr := strings.TrimPrefix(endpoint, "rdma://")

	// Connect
	conn, err := c.transport.Connect(ctx, addr)
	if err != nil {
		return nil, err
	}

	c.conns[endpoint] = conn
	c.ops[endpoint] = NewS3Operations(c.transport, conn)

	return conn, nil
}

// getOperations gets S3 operations handler for an endpoint.
func (c *Client) getOperations(ctx context.Context) (*S3Operations, error) {
	// Select best endpoint (could implement load balancing)
	endpoint := c.endpoints[0]

	c.mu.RLock()
	ops, exists := c.ops[endpoint]
	c.mu.RUnlock()

	if exists {
		return ops, nil
	}

	// Create connection and operations
	_, err := c.getConnection(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	c.mu.RLock()
	ops = c.ops[endpoint]
	c.mu.RUnlock()

	return ops, nil
}

// useRDMA checks if RDMA should be used.
func (c *Client) useRDMA() bool {
	return c.transport != nil && c.transport.IsAvailable()
}

// GetObject retrieves an object.
func (c *Client) GetObject(ctx context.Context, bucket, key string) (*GetObjectResult, error) {
	return c.GetObjectWithOptions(ctx, bucket, key, nil)
}

// GetObjectWithOptions retrieves an object with options.
func (c *Client) GetObjectWithOptions(ctx context.Context, bucket, key string, opts *GetObjectOptions) (*GetObjectResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	var lastErr error

	for attempt := range c.config.RetryAttempts {
		if attempt > 0 {
			time.Sleep(c.config.RetryDelay * time.Duration(attempt))
		}

		if c.useRDMA() {
			ops, err := c.getOperations(ctx)
			if err != nil {
				lastErr = err
				continue
			}

			output, err := ops.GetObject(ctx, bucket, key, opts)
			if err != nil {
				lastErr = err
				continue
			}

			return &GetObjectResult{
				Body:        output.Body,
				ContentType: output.ContentType,
				Metadata:    output.Metadata,
				ETag:        output.ETag,
				Size:        output.Size,
				Latency:     output.Latency,
				Transport:   transportTypeRDMA,
			}, nil
		}

		// Fall back to HTTP
		if c.config.FallbackHTTP {
			return c.getObjectHTTP(ctx, bucket, key, opts)
		}

		return nil, ErrRDMANotAvailable
	}

	return nil, fmt.Errorf("all retry attempts failed: %w", lastErr)
}

// GetObjectResult contains the result of GetObject.
type GetObjectResult struct {
	Body        io.Reader
	Metadata    map[string]string
	ContentType string
	ETag        string
	Transport   string
	Size        int64
	Latency     time.Duration
}

// getObjectHTTP performs GetObject via HTTP fallback.
func (c *Client) getObjectHTTP(ctx context.Context, bucket, key string, opts *GetObjectOptions) (*GetObjectResult, error) {
	if c.config.HTTPEndpoint == "" {
		return nil, errors.New("HTTP fallback endpoint not configured")
	}

	url := fmt.Sprintf("%s/%s/%s", c.config.HTTPEndpoint, bucket, key)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	return &GetObjectResult{
		Body:        resp.Body,
		ContentType: resp.Header.Get("Content-Type"),
		ETag:        resp.Header.Get("ETag"),
		Size:        resp.ContentLength,
		Latency:     time.Since(start),
		Transport:   transportTypeHTTP,
	}, nil
}

// PutObject uploads an object.
func (c *Client) PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) (*PutObjectResult, error) {
	return c.PutObjectWithOptions(ctx, bucket, key, body, size, nil)
}

// PutObjectWithOptions uploads an object with options.
func (c *Client) PutObjectWithOptions(ctx context.Context, bucket, key string, body io.Reader, size int64, opts *PutObjectOptions) (*PutObjectResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	if opts == nil {
		opts = &PutObjectOptions{}
	}

	opts.ContentLength = size

	var lastErr error

	for attempt := range c.config.RetryAttempts {
		if attempt > 0 {
			time.Sleep(c.config.RetryDelay * time.Duration(attempt))
		}

		if c.useRDMA() {
			ops, err := c.getOperations(ctx)
			if err != nil {
				lastErr = err
				continue
			}

			output, err := ops.PutObject(ctx, bucket, key, body, opts)
			if err != nil {
				lastErr = err
				continue
			}

			return &PutObjectResult{
				ETag:      output.ETag,
				Latency:   output.Latency,
				Transport: transportTypeRDMA,
			}, nil
		}

		// Fall back to HTTP
		if c.config.FallbackHTTP {
			return c.putObjectHTTP(ctx, bucket, key, body, size, opts)
		}

		return nil, ErrRDMANotAvailable
	}

	return nil, fmt.Errorf("all retry attempts failed: %w", lastErr)
}

// PutObjectResult contains the result of PutObject.
type PutObjectResult struct {
	ETag      string
	Transport string
	Latency   time.Duration
}

// putObjectHTTP performs PutObject via HTTP fallback.
func (c *Client) putObjectHTTP(ctx context.Context, bucket, key string, body io.Reader, size int64, opts *PutObjectOptions) (*PutObjectResult, error) {
	if c.config.HTTPEndpoint == "" {
		return nil, errors.New("HTTP fallback endpoint not configured")
	}

	url := fmt.Sprintf("%s/%s/%s", c.config.HTTPEndpoint, bucket, key)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		return nil, err
	}

	req.ContentLength = size
	if opts != nil && opts.ContentType != "" {
		req.Header.Set("Content-Type", opts.ContentType)
	}

	start := time.Now()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	return &PutObjectResult{
		ETag:      resp.Header.Get("ETag"),
		Latency:   time.Since(start),
		Transport: transportTypeHTTP,
	}, nil
}

// DeleteObject deletes an object.
func (c *Client) DeleteObject(ctx context.Context, bucket, key string) error {
	ctx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	if c.useRDMA() {
		ops, err := c.getOperations(ctx)
		if err != nil {
			return err
		}

		_, err = ops.DeleteObject(ctx, bucket, key, nil)

		return err
	}

	if c.config.FallbackHTTP {
		return c.deleteObjectHTTP(ctx, bucket, key)
	}

	return ErrRDMANotAvailable
}

// deleteObjectHTTP performs DeleteObject via HTTP fallback.
func (c *Client) deleteObjectHTTP(ctx context.Context, bucket, key string) error {
	if c.config.HTTPEndpoint == "" {
		return errors.New("HTTP fallback endpoint not configured")
	}

	url := fmt.Sprintf("%s/%s/%s", c.config.HTTPEndpoint, bucket, key)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("HTTP error: %s", resp.Status)
	}

	return nil
}

// HeadObject gets object metadata.
func (c *Client) HeadObject(ctx context.Context, bucket, key string) (*HeadObjectResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	if c.useRDMA() {
		ops, err := c.getOperations(ctx)
		if err != nil {
			return nil, err
		}

		output, err := ops.HeadObject(ctx, bucket, key, nil)
		if err != nil {
			return nil, err
		}

		return &HeadObjectResult{
			ContentType: output.ContentType,
			Metadata:    output.Metadata,
			ETag:        output.ETag,
			Size:        output.Size,
			Latency:     output.Latency,
			Transport:   transportTypeRDMA,
		}, nil
	}

	if c.config.FallbackHTTP {
		return c.headObjectHTTP(ctx, bucket, key)
	}

	return nil, ErrRDMANotAvailable
}

// HeadObjectResult contains the result of HeadObject.
type HeadObjectResult struct {
	Metadata    map[string]string
	ContentType string
	ETag        string
	Transport   string
	Size        int64
	Latency     time.Duration
}

// headObjectHTTP performs HeadObject via HTTP fallback.
func (c *Client) headObjectHTTP(ctx context.Context, bucket, key string) (*HeadObjectResult, error) {
	if c.config.HTTPEndpoint == "" {
		return nil, errors.New("HTTP fallback endpoint not configured")
	}

	url := fmt.Sprintf("%s/%s/%s", c.config.HTTPEndpoint, bucket, key)

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %s", resp.Status)
	}

	return &HeadObjectResult{
		ContentType: resp.Header.Get("Content-Type"),
		ETag:        resp.Header.Get("ETag"),
		Size:        resp.ContentLength,
		Latency:     time.Since(start),
		Transport:   transportTypeHTTP,
	}, nil
}

// ListObjects lists objects in a bucket.
func (c *Client) ListObjects(ctx context.Context, bucket string, opts *ListObjectsOptions) (*ListObjectsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.config.RequestTimeout)
	defer cancel()

	if c.useRDMA() {
		ops, err := c.getOperations(ctx)
		if err != nil {
			return nil, err
		}

		output, err := ops.ListObjects(ctx, bucket, opts)
		if err != nil {
			return nil, err
		}

		return &ListObjectsResult{
			Objects:               output.Objects,
			CommonPrefixes:        output.CommonPrefixes,
			NextContinuationToken: output.NextContinuationToken,
			IsTruncated:           output.IsTruncated,
			Latency:               output.Latency,
			Transport:             transportTypeRDMA,
		}, nil
	}

	if c.config.FallbackHTTP {
		return c.listObjectsHTTP(ctx, bucket, opts)
	}

	return nil, ErrRDMANotAvailable
}

// ListObjectsResult contains the result of ListObjects.
type ListObjectsResult struct {
	NextContinuationToken string
	Transport             string
	Objects               []ObjectInfo
	CommonPrefixes        []string
	Latency               time.Duration
	IsTruncated           bool
}

// listObjectsHTTP performs ListObjects via HTTP fallback.
func (c *Client) listObjectsHTTP(ctx context.Context, bucket string, opts *ListObjectsOptions) (*ListObjectsResult, error) {
	// Simplified - real implementation would parse XML response
	return nil, errors.New("HTTP ListObjects not implemented")
}

// AllocateBuffer allocates a memory region from the RDMA pool for zero-copy operations.
func (c *Client) AllocateBuffer(ctx context.Context, size int) (*MemoryRegion, error) {
	if !c.useRDMA() {
		// Return a regular buffer for non-RDMA
		return &MemoryRegion{
			Buffer: make([]byte, size),
			Length: uint64(size), //nolint:gosec // G115: size is validated positive
		}, nil
	}

	return c.transport.memPool.GetRegion(ctx)
}

// ReleaseBuffer releases a memory region back to the pool.
func (c *Client) ReleaseBuffer(mr *MemoryRegion) {
	if mr == nil {
		return
	}

	if c.useRDMA() && mr.pool != nil {
		c.transport.memPool.ReleaseRegion(mr)
	}
	// For non-pool buffers, let GC handle it
}

// ZeroCopyGetObject performs a zero-copy GetObject (RDMA only).
func (c *Client) ZeroCopyGetObject(ctx context.Context, bucket, key string, buffer *MemoryRegion) (*GetObjectResult, error) {
	if !c.useRDMA() {
		return nil, errors.New("zero-copy requires RDMA")
	}

	ops, err := c.getOperations(ctx)
	if err != nil {
		return nil, err
	}

	output, err := ops.ZeroCopyGetObject(ctx, bucket, key, buffer)
	if err != nil {
		return nil, err
	}

	return &GetObjectResult{
		Body:        output.Body,
		ContentType: output.ContentType,
		Metadata:    output.Metadata,
		ETag:        output.ETag,
		Size:        output.Size,
		Latency:     output.Latency,
		Transport:   "rdma-zerocopy",
	}, nil
}

// ZeroCopyPutObject performs a zero-copy PutObject (RDMA only).
func (c *Client) ZeroCopyPutObject(ctx context.Context, bucket, key string, buffer *MemoryRegion, size int64, opts *PutObjectOptions) (*PutObjectResult, error) {
	if !c.useRDMA() {
		return nil, errors.New("zero-copy requires RDMA")
	}

	ops, err := c.getOperations(ctx)
	if err != nil {
		return nil, err
	}

	output, err := ops.ZeroCopyPutObject(ctx, bucket, key, buffer, size, opts)
	if err != nil {
		return nil, err
	}

	return &PutObjectResult{
		ETag:      output.ETag,
		Latency:   output.Latency,
		Transport: "rdma-zerocopy",
	}, nil
}

// GetMetrics returns client metrics.
func (c *Client) GetMetrics() *ClientMetrics {
	metrics := &ClientMetrics{}

	if c.useRDMA() {
		tm := c.transport.GetMetrics()
		metrics.ConnectionsActive = tm.ConnectionsActive
		metrics.BytesSent = tm.BytesSent
		metrics.BytesReceived = tm.BytesReceived
		metrics.OperationsTotal = tm.OperationsTotal
		metrics.OperationsSuccess = tm.OperationsSuccess
		metrics.OperationsFailed = tm.OperationsFailed
		metrics.AverageLatency = tm.AverageLatency()
		metrics.MemoryUsed = tm.MemoryUsed
		metrics.MemoryTotal = tm.MemoryTotal
		metrics.Transport = transportTypeRDMA
	} else {
		metrics.Transport = transportTypeHTTP
	}

	return metrics
}

// ClientMetrics contains client performance metrics.
type ClientMetrics struct {
	Transport         string
	ConnectionsActive int64
	BytesSent         int64
	BytesReceived     int64
	OperationsTotal   int64
	OperationsSuccess int64
	OperationsFailed  int64
	AverageLatency    time.Duration
	MemoryUsed        int64
	MemoryTotal       int64
}

// Close closes the client and releases resources.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true

	// Close all connections
	for endpoint := range c.conns {
		if c.transport != nil {
			_ = c.transport.Disconnect(endpoint)
		}
	}

	c.conns = nil
	c.ops = nil

	// Close transport
	if c.transport != nil {
		_ = c.transport.Close()
	}

	return nil
}

// IsRDMAEnabled returns true if RDMA is enabled and available.
func (c *Client) IsRDMAEnabled() bool {
	return c.useRDMA()
}

// GetTransportType returns the active transport type.
func (c *Client) GetTransportType() string {
	if c.useRDMA() {
		return transportTypeRDMA
	}

	return transportTypeHTTP
}
