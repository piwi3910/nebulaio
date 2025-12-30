package rdma

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// S3Operations provides S3 API operations over RDMA transport.
type S3Operations struct {
	transport *Transport
	conn      *Connection
}

// NewS3Operations creates a new S3 operations handler for an RDMA connection.
func NewS3Operations(transport *Transport, conn *Connection) *S3Operations {
	return &S3Operations{
		transport: transport,
		conn:      conn,
	}
}

// GetObject retrieves an object over RDMA.
func (s *S3Operations) GetObject(ctx context.Context, bucket, key string, opts *GetObjectOptions) (*GetObjectOutput, error) {
	if opts == nil {
		opts = &GetObjectOptions{}
	}

	req := &S3Request{
		OpCode:    OpGetObject,
		Bucket:    bucket,
		Key:       key,
		VersionID: opts.VersionID,
	}

	// For zero-copy reads, allocate buffer from pool
	if s.transport.config.EnableZeroCopy && opts.Buffer != nil {
		req.LocalBuffer = opts.Buffer
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GetObject failed with status %d", resp.StatusCode)
	}

	return &GetObjectOutput{
		Body:        resp.Body,
		ContentType: resp.ContentType,
		Metadata:    resp.Metadata,
		ETag:        resp.ETag,
		Size:        resp.BodySize,
		Latency:     resp.Latency,
	}, nil
}

// GetObjectOptions contains options for GetObject.
type GetObjectOptions struct {
	Range     *ByteRange
	Buffer    *MemoryRegion
	VersionID string
}

// ByteRange represents a byte range for partial object reads.
type ByteRange struct {
	Start int64
	End   int64
}

// GetObjectOutput contains the response from GetObject.
type GetObjectOutput struct {
	Body        io.Reader
	ContentType string
	Metadata    map[string]string
	ETag        string
	Size        int64
	Latency     time.Duration
}

// PutObject uploads an object over RDMA.
func (s *S3Operations) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts *PutObjectOptions) (*PutObjectOutput, error) {
	if opts == nil {
		opts = &PutObjectOptions{}
	}

	// Read body size
	var bodySize int64
	if opts.ContentLength > 0 {
		bodySize = opts.ContentLength
	} else if sr, ok := body.(io.Seeker); ok {
		// Seek to end to get size
		end, err := sr.Seek(0, io.SeekEnd)
		if err == nil {
			bodySize = end
			_, _ = sr.Seek(0, io.SeekStart)
		}
	}

	req := &S3Request{
		OpCode:      OpPutObject,
		Bucket:      bucket,
		Key:         key,
		ContentType: opts.ContentType,
		Metadata:    opts.Metadata,
		Body:        body,
		BodySize:    bodySize,
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PutObject failed with status %d", resp.StatusCode)
	}

	return &PutObjectOutput{
		ETag:    resp.ETag,
		Latency: resp.Latency,
	}, nil
}

// PutObjectOptions contains options for PutObject.
type PutObjectOptions struct {
	Metadata      map[string]string
	Buffer        *MemoryRegion
	ContentType   string
	ContentLength int64
}

// PutObjectOutput contains the response from PutObject.
type PutObjectOutput struct {
	ETag    string
	Latency time.Duration
}

// DeleteObject deletes an object over RDMA.
func (s *S3Operations) DeleteObject(ctx context.Context, bucket, key string, opts *DeleteObjectOptions) (*DeleteObjectOutput, error) {
	if opts == nil {
		opts = &DeleteObjectOptions{}
	}

	req := &S3Request{
		OpCode:    OpDeleteObject,
		Bucket:    bucket,
		Key:       key,
		VersionID: opts.VersionID,
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("DeleteObject failed with status %d", resp.StatusCode)
	}

	return &DeleteObjectOutput{
		Latency: resp.Latency,
	}, nil
}

// DeleteObjectOptions contains options for DeleteObject.
type DeleteObjectOptions struct {
	VersionID string
}

// DeleteObjectOutput contains the response from DeleteObject.
type DeleteObjectOutput struct {
	Latency time.Duration
}

// HeadObject gets object metadata over RDMA.
func (s *S3Operations) HeadObject(ctx context.Context, bucket, key string, opts *HeadObjectOptions) (*HeadObjectOutput, error) {
	if opts == nil {
		opts = &HeadObjectOptions{}
	}

	req := &S3Request{
		OpCode:    OpHeadObject,
		Bucket:    bucket,
		Key:       key,
		VersionID: opts.VersionID,
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HeadObject failed with status %d", resp.StatusCode)
	}

	return &HeadObjectOutput{
		ContentType: resp.ContentType,
		Metadata:    resp.Metadata,
		ETag:        resp.ETag,
		Size:        resp.BodySize,
		Latency:     resp.Latency,
	}, nil
}

// HeadObjectOptions contains options for HeadObject.
type HeadObjectOptions struct {
	VersionID string
}

// HeadObjectOutput contains the response from HeadObject.
type HeadObjectOutput struct {
	ContentType string
	Metadata    map[string]string
	ETag        string
	Size        int64
	Latency     time.Duration
}

// ListObjects lists objects over RDMA.
func (s *S3Operations) ListObjects(ctx context.Context, bucket string, opts *ListObjectsOptions) (*ListObjectsOutput, error) {
	if opts == nil {
		opts = &ListObjectsOptions{}
	}

	// Encode list parameters in metadata
	metadata := map[string]string{}
	if opts.Prefix != "" {
		metadata["prefix"] = opts.Prefix
	}

	if opts.Delimiter != "" {
		metadata["delimiter"] = opts.Delimiter
	}

	if opts.MaxKeys > 0 {
		metadata["max-keys"] = strconv.Itoa(opts.MaxKeys)
	}

	if opts.ContinuationToken != "" {
		metadata["continuation-token"] = opts.ContinuationToken
	}

	req := &S3Request{
		OpCode:   OpListObjects,
		Bucket:   bucket,
		Metadata: metadata,
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ListObjects failed with status %d", resp.StatusCode)
	}

	// Parse response body for object list
	output := &ListObjectsOutput{
		Latency: resp.Latency,
	}

	if resp.Body != nil {
		// Parse binary list format
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		output.Objects, output.CommonPrefixes, output.NextContinuationToken, output.IsTruncated = parseListResponse(data)
	}

	return output, nil
}

// ListObjectsOptions contains options for ListObjects.
type ListObjectsOptions struct {
	Prefix            string
	Delimiter         string
	ContinuationToken string
	MaxKeys           int
}

// ListObjectsOutput contains the response from ListObjects.
type ListObjectsOutput struct {
	NextContinuationToken string
	Objects               []ObjectInfo
	CommonPrefixes        []string
	Latency               time.Duration
	IsTruncated           bool
}

// ObjectInfo contains information about an object.
type ObjectInfo struct {
	LastModified time.Time
	Key          string
	ETag         string
	StorageClass string
	Size         int64
}

func parseListResponse(data []byte) ([]ObjectInfo, []string, string, bool) {
	// Simple binary format for list response
	// [Count:4][IsTruncated:1][NextTokenLen:2][NextToken][Objects...]
	// Each object: [KeyLen:2][Key][Size:8][ETagLen:2][ETag][Timestamp:8]
	if len(data) < 7 {
		return nil, nil, "", false
	}

	r := bytes.NewReader(data)

	var count uint32
	binary.Read(r, binary.BigEndian, &count)

	var isTruncated uint8
	binary.Read(r, binary.BigEndian, &isTruncated)

	var nextTokenLen uint16

	_ = binary.Read(r, binary.BigEndian, &nextTokenLen)
	nextToken := make([]byte, nextTokenLen)
	_, _ = r.Read(nextToken)

	objects := make([]ObjectInfo, 0, count)

	for range count {
		var keyLen uint16

		err := binary.Read(r, binary.BigEndian, &keyLen)
		if err != nil {
			break
		}

		key := make([]byte, keyLen)
		_, _ = r.Read(key)

		var size int64

		_ = binary.Read(r, binary.BigEndian, &size)

		var etagLen uint16

		_ = binary.Read(r, binary.BigEndian, &etagLen)
		etag := make([]byte, etagLen)
		_, _ = r.Read(etag)

		var timestamp int64
		binary.Read(r, binary.BigEndian, &timestamp)

		objects = append(objects, ObjectInfo{
			Key:          string(key),
			Size:         size,
			ETag:         string(etag),
			LastModified: time.Unix(timestamp, 0),
		})
	}

	return objects, nil, string(nextToken), isTruncated == 1
}

// CreateMultipartUpload initiates a multipart upload over RDMA.
func (s *S3Operations) CreateMultipartUpload(ctx context.Context, bucket, key string, opts *CreateMultipartUploadOptions) (*CreateMultipartUploadOutput, error) {
	if opts == nil {
		opts = &CreateMultipartUploadOptions{}
	}

	req := &S3Request{
		OpCode:      OpCreateMultipartUpload,
		Bucket:      bucket,
		Key:         key,
		ContentType: opts.ContentType,
		Metadata:    opts.Metadata,
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CreateMultipartUpload failed with status %d", resp.StatusCode)
	}

	// Extract upload ID from response
	var uploadID string

	if resp.Body != nil {
		data, _ := io.ReadAll(resp.Body)
		uploadID = string(data)
	}

	return &CreateMultipartUploadOutput{
		UploadID: uploadID,
		Latency:  resp.Latency,
	}, nil
}

// CreateMultipartUploadOptions contains options for CreateMultipartUpload.
type CreateMultipartUploadOptions struct {
	Metadata    map[string]string
	ContentType string
}

// CreateMultipartUploadOutput contains the response from CreateMultipartUpload.
type CreateMultipartUploadOutput struct {
	UploadID string
	Latency  time.Duration
}

// UploadPart uploads a part over RDMA.
func (s *S3Operations) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader, opts *UploadPartOptions) (*UploadPartOutput, error) {
	if opts == nil {
		opts = &UploadPartOptions{}
	}

	// Read body size
	var bodySize int64
	if opts.ContentLength > 0 {
		bodySize = opts.ContentLength
	}

	// Encode upload metadata
	metadata := map[string]string{
		"upload-id":   uploadID,
		"part-number": strconv.Itoa(partNumber),
	}

	req := &S3Request{
		OpCode:   OpUploadPart,
		Bucket:   bucket,
		Key:      key,
		Metadata: metadata,
		Body:     body,
		BodySize: bodySize,
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("UploadPart failed with status %d", resp.StatusCode)
	}

	return &UploadPartOutput{
		ETag:    resp.ETag,
		Latency: resp.Latency,
	}, nil
}

// UploadPartOptions contains options for UploadPart.
type UploadPartOptions struct {
	Buffer        *MemoryRegion
	ContentLength int64
}

// UploadPartOutput contains the response from UploadPart.
type UploadPartOutput struct {
	ETag    string
	Latency time.Duration
}

// CompleteMultipartUpload completes a multipart upload over RDMA.
func (s *S3Operations) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) (*CompleteMultipartUploadOutput, error) {
	// Encode parts in body
	var buf bytes.Buffer
	for _, part := range parts {
		//nolint:gosec // G115: part.PartNumber is bounded by S3 multipart limits
		binary.Write(&buf, binary.BigEndian, int32(part.PartNumber))
		etagBytes := []byte(part.ETag)
		//nolint:gosec // G115: ETag length is bounded by S3 spec
		binary.Write(&buf, binary.BigEndian, uint16(len(etagBytes)))
		buf.Write(etagBytes)
	}

	metadata := map[string]string{
		"upload-id": uploadID,
	}

	req := &S3Request{
		OpCode:   OpCompleteMultipartUpload,
		Bucket:   bucket,
		Key:      key,
		Metadata: metadata,
		Body:     &buf,
		BodySize: int64(buf.Len()),
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("CompleteMultipartUpload failed with status %d", resp.StatusCode)
	}

	return &CompleteMultipartUploadOutput{
		ETag:    resp.ETag,
		Latency: resp.Latency,
	}, nil
}

// CompletedPart represents a completed part in a multipart upload.
type CompletedPart struct {
	ETag       string
	PartNumber int
}

// CompleteMultipartUploadOutput contains the response from CompleteMultipartUpload.
type CompleteMultipartUploadOutput struct {
	ETag    string
	Latency time.Duration
}

// AbortMultipartUpload aborts a multipart upload over RDMA.
func (s *S3Operations) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) (*AbortMultipartUploadOutput, error) {
	metadata := map[string]string{
		"upload-id": uploadID,
	}

	req := &S3Request{
		OpCode:   OpAbortMultipartUpload,
		Bucket:   bucket,
		Key:      key,
		Metadata: metadata,
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return nil, fmt.Errorf("AbortMultipartUpload failed with status %d", resp.StatusCode)
	}

	return &AbortMultipartUploadOutput{
		Latency: resp.Latency,
	}, nil
}

// AbortMultipartUploadOutput contains the response from AbortMultipartUpload.
type AbortMultipartUploadOutput struct {
	Latency time.Duration
}

// ZeroCopyGetObject performs a zero-copy GetObject using RDMA READ.
func (s *S3Operations) ZeroCopyGetObject(ctx context.Context, bucket, key string, buffer *MemoryRegion) (*GetObjectOutput, error) {
	if buffer == nil {
		return nil, ErrBufferTooSmall
	}

	// For zero-copy, we use RDMA READ directly into the provided buffer
	// The server will RDMA WRITE the data directly to client memory

	req := &S3Request{
		OpCode:      OpGetObject,
		Bucket:      bucket,
		Key:         key,
		LocalBuffer: buffer,
		RemoteAddr:  buffer.Address,
		RemoteKey:   buffer.RemoteKey,
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	// Data is already in the buffer via RDMA WRITE
	return &GetObjectOutput{
		Body:        bytes.NewReader(buffer.Buffer[:resp.BodySize]),
		ContentType: resp.ContentType,
		Metadata:    resp.Metadata,
		ETag:        resp.ETag,
		Size:        resp.BodySize,
		Latency:     resp.Latency,
	}, nil
}

// ZeroCopyPutObject performs a zero-copy PutObject using RDMA READ.
func (s *S3Operations) ZeroCopyPutObject(ctx context.Context, bucket, key string, buffer *MemoryRegion, size int64, opts *PutObjectOptions) (*PutObjectOutput, error) {
	if buffer == nil {
		return nil, ErrBufferTooSmall
	}

	if opts == nil {
		opts = &PutObjectOptions{}
	}

	// For zero-copy, the server performs RDMA READ from client memory
	req := &S3Request{
		OpCode:      OpPutObject,
		Bucket:      bucket,
		Key:         key,
		ContentType: opts.ContentType,
		Metadata:    opts.Metadata,
		LocalBuffer: buffer,
		RemoteAddr:  buffer.Address,
		RemoteKey:   buffer.RemoteKey,
		BodySize:    size,
	}

	resp, err := s.conn.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	return &PutObjectOutput{
		ETag:    resp.ETag,
		Latency: resp.Latency,
	}, nil
}

// BatchGetObjects performs batch GetObject operations for improved throughput.
func (s *S3Operations) BatchGetObjects(ctx context.Context, bucket string, keys []string) ([]*GetObjectOutput, error) {
	results := make([]*GetObjectOutput, len(keys))
	errors := make([]error, len(keys))

	// For true RDMA batching, we would:
	// 1. Pre-post multiple receive buffers
	// 2. Send batch request
	// 3. Receive all responses in parallel

	// For now, sequential with potential for parallelization
	for i, key := range keys {
		output, err := s.GetObject(ctx, bucket, key, nil)
		results[i] = output
		errors[i] = err
	}

	// Check for any errors
	for _, err := range errors {
		if err != nil {
			return results, fmt.Errorf("batch get had errors: %w", err)
		}
	}

	return results, nil
}

// StreamGetObject streams an object with prefetching for sequential access.
func (s *S3Operations) StreamGetObject(ctx context.Context, bucket, key string, chunkSize int) (<-chan []byte, <-chan error) {
	dataChan := make(chan []byte, 4) // Buffer a few chunks
	errChan := make(chan error, 1)

	go func() {
		defer close(dataChan)
		defer close(errChan)

		// Get object metadata first
		head, err := s.HeadObject(ctx, bucket, key, nil)
		if err != nil {
			errChan <- err
			return
		}

		totalSize := head.Size

		if chunkSize <= 0 {
			chunkSize = 1 << 20 // 1MB default chunks
		}

		// Read in chunks
		for offset := int64(0); offset < totalSize; offset += int64(chunkSize) {
			endOffset := offset + int64(chunkSize)
			if endOffset > totalSize {
				endOffset = totalSize
			}

			// Get chunk with range
			output, err := s.GetObject(ctx, bucket, key, &GetObjectOptions{
				Range: &ByteRange{Start: offset, End: endOffset - 1},
			})
			if err != nil {
				errChan <- err
				return
			}

			data, err := io.ReadAll(output.Body)
			if err != nil {
				errChan <- err
				return
			}

			select {
			case dataChan <- data:
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			}
		}
	}()

	return dataChan, errChan
}
