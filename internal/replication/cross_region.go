// Package replication provides cross-region replication functionality for NebulaIO.
package replication

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// S3ReplicationStatus represents the status of replication for S3 API responses.
type S3ReplicationStatus string

const (
	S3ReplicationStatusPending   S3ReplicationStatus = "PENDING"
	S3ReplicationStatusCompleted S3ReplicationStatus = "COMPLETED"
	S3ReplicationStatusFailed    S3ReplicationStatus = "FAILED"
	S3ReplicationStatusReplica   S3ReplicationStatus = "REPLICA"
)

// ReplicationRule defines a replication rule.
type ReplicationRule struct {
	ID                      string                   `xml:"ID" json:"id"`
	Priority                int                      `xml:"Priority" json:"priority"`
	Status                  string                   `xml:"Status" json:"status"` // Enabled or Disabled
	Filter                  *ReplicationFilter       `xml:"Filter,omitempty" json:"filter,omitempty"`
	Destination             ReplicationDestination   `xml:"Destination" json:"destination"`
	DeleteMarkerReplication *S3DeleteMarkerReplication `xml:"DeleteMarkerReplication,omitempty" json:"deleteMarkerReplication,omitempty"`
	SourceSelectionCriteria *SourceSelectionCriteria `xml:"SourceSelectionCriteria,omitempty" json:"sourceSelectionCriteria,omitempty"`
	ExistingObjectReplication *S3ExistingObjectReplication `xml:"ExistingObjectReplication,omitempty" json:"existingObjectReplication,omitempty"`
}

// ReplicationFilter defines which objects to replicate.
type ReplicationFilter struct {
	Prefix string         `xml:"Prefix,omitempty" json:"prefix,omitempty"`
	Tag    *ReplicationTag `xml:"Tag,omitempty" json:"tag,omitempty"`
	And    *ReplicationAnd `xml:"And,omitempty" json:"and,omitempty"`
}

// ReplicationTag defines a tag filter.
type ReplicationTag struct {
	Key   string `xml:"Key" json:"key"`
	Value string `xml:"Value" json:"value"`
}

// ReplicationAnd combines multiple filter conditions.
type ReplicationAnd struct {
	Prefix string           `xml:"Prefix,omitempty" json:"prefix,omitempty"`
	Tags   []ReplicationTag `xml:"Tag,omitempty" json:"tags,omitempty"`
}

// ReplicationDestination defines where to replicate.
type ReplicationDestination struct {
	Bucket              string              `xml:"Bucket" json:"bucket"`
	Account             string              `xml:"Account,omitempty" json:"account,omitempty"`
	StorageClass        string              `xml:"StorageClass,omitempty" json:"storageClass,omitempty"`
	AccessControlTranslation *AccessControlTranslation `xml:"AccessControlTranslation,omitempty" json:"accessControlTranslation,omitempty"`
	EncryptionConfiguration *EncryptionConfiguration `xml:"EncryptionConfiguration,omitempty" json:"encryptionConfiguration,omitempty"`
	Metrics             *S3ReplicationMetrics `xml:"Metrics,omitempty" json:"metrics,omitempty"`
	ReplicationTime     *ReplicationTime    `xml:"ReplicationTime,omitempty" json:"replicationTime,omitempty"`
}

// AccessControlTranslation controls ACL translation.
type AccessControlTranslation struct {
	Owner string `xml:"Owner" json:"owner"` // Destination
}

// EncryptionConfiguration specifies encryption for replicated objects.
type EncryptionConfiguration struct {
	ReplicaKmsKeyID string `xml:"ReplicaKmsKeyID,omitempty" json:"replicaKmsKeyId,omitempty"`
}

// S3ReplicationMetrics controls replication metrics for S3 API.
type S3ReplicationMetrics struct {
	Status        string                   `xml:"Status" json:"status"` // Enabled or Disabled
	EventThreshold *ReplicationEventThreshold `xml:"EventThreshold,omitempty" json:"eventThreshold,omitempty"`
}

// ReplicationEventThreshold defines replication event thresholds.
type ReplicationEventThreshold struct {
	Minutes int `xml:"Minutes" json:"minutes"`
}

// ReplicationTime controls replication time.
type ReplicationTime struct {
	Status string           `xml:"Status" json:"status"` // Enabled or Disabled
	Time   *ReplicationTimeValue `xml:"Time,omitempty" json:"time,omitempty"`
}

// ReplicationTimeValue defines replication time constraints.
type ReplicationTimeValue struct {
	Minutes int `xml:"Minutes" json:"minutes"`
}

// S3DeleteMarkerReplication controls delete marker replication for S3 API.
type S3DeleteMarkerReplication struct {
	Status string `xml:"Status" json:"status"` // Enabled or Disabled
}

// SourceSelectionCriteria specifies source selection criteria.
type SourceSelectionCriteria struct {
	SseKmsEncryptedObjects *SseKmsEncryptedObjects `xml:"SseKmsEncryptedObjects,omitempty" json:"sseKmsEncryptedObjects,omitempty"`
	ReplicaModifications   *ReplicaModifications   `xml:"ReplicaModifications,omitempty" json:"replicaModifications,omitempty"`
}

// SseKmsEncryptedObjects controls replication of KMS-encrypted objects.
type SseKmsEncryptedObjects struct {
	Status string `xml:"Status" json:"status"` // Enabled or Disabled
}

// ReplicaModifications controls replication of replica modifications.
type ReplicaModifications struct {
	Status string `xml:"Status" json:"status"` // Enabled or Disabled
}

// S3ExistingObjectReplication controls replication of existing objects for S3 API.
type S3ExistingObjectReplication struct {
	Status string `xml:"Status" json:"status"` // Enabled or Disabled
}

// ReplicationConfiguration represents the complete replication configuration.
type ReplicationConfiguration struct {
	Role  string            `xml:"Role" json:"role"`
	Rules []ReplicationRule `xml:"Rule" json:"rules"`
}

// ReplicationTask represents a pending replication task.
type ReplicationTask struct {
	ID              string
	SourceBucket    string
	SourceKey       string
	SourceVersionID string
	DestBucket      string
	DestKey         string
	DestRegion      string
	Rule            *ReplicationRule
	Status          S3ReplicationStatus
	Attempts        int
	LastAttempt     time.Time
	Error           string
	CreatedAt       time.Time
	Size            int64
	ETag            string
}

// ReplicationManager manages cross-region replication.
type ReplicationManager struct {
	mu              sync.RWMutex
	configs         map[string]*ReplicationConfiguration // bucket -> config
	tasks           chan *ReplicationTask
	activeWorkers   int32
	maxWorkers      int
	endpoints       map[string]*RegionEndpoint // region -> endpoint
	storage         ReplicationStorage
	metrics         *ReplicationMetricsCollector
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
}

// RegionEndpoint represents a remote region endpoint.
type RegionEndpoint struct {
	Region    string
	Endpoint  string
	AccessKey string
	SecretKey string
	Secure    bool
}

// ReplicationStorage interface for accessing storage.
type ReplicationStorage interface {
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, *S3ObjectInfo, error)
	GetObjectVersion(ctx context.Context, bucket, key, versionID string) (io.ReadCloser, *S3ObjectInfo, error)
	PutObject(ctx context.Context, bucket, key string, reader io.Reader, size int64, opts *PutOptions) error
	DeleteObject(ctx context.Context, bucket, key string) error
	GetObjectTags(ctx context.Context, bucket, key string) (map[string]string, error)
}

// S3ObjectInfo contains object metadata for S3 replication.
type S3ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	VersionID    string
	Metadata     map[string]string
	StorageClass string
}

// PutOptions contains options for PutObject.
type PutOptions struct {
	ContentType  string
	Metadata     map[string]string
	StorageClass string
	SSE          string
	Tagging      map[string]string
}

// ReplicationMetricsCollector collects replication metrics.
type ReplicationMetricsCollector struct {
	mu                  sync.Mutex
	objectsReplicated   int64
	bytesReplicated     int64
	objectsFailed       int64
	pendingCount        int64
	replicationLatency  []time.Duration
	lastReplicationTime time.Time
}

// NewReplicationManager creates a new replication manager.
func NewReplicationManager(storage ReplicationStorage, maxWorkers int) *ReplicationManager {
	ctx, cancel := context.WithCancel(context.Background())

	rm := &ReplicationManager{
		configs:    make(map[string]*ReplicationConfiguration),
		tasks:      make(chan *ReplicationTask, 10000),
		maxWorkers: maxWorkers,
		endpoints:  make(map[string]*RegionEndpoint),
		storage:    storage,
		metrics:    &ReplicationMetricsCollector{},
		ctx:        ctx,
		cancel:     cancel,
	}

	// Start worker pool
	for i := 0; i < maxWorkers; i++ {
		rm.wg.Add(1)
		go rm.worker()
	}

	return rm
}

// SetReplicationConfiguration sets replication configuration for a bucket.
func (rm *ReplicationManager) SetReplicationConfiguration(bucket string, config *ReplicationConfiguration) error {
	if err := rm.validateConfiguration(config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	rm.mu.Lock()
	rm.configs[bucket] = config
	rm.mu.Unlock()

	return nil
}

// GetReplicationConfiguration gets replication configuration for a bucket.
func (rm *ReplicationManager) GetReplicationConfiguration(bucket string) (*ReplicationConfiguration, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	config, exists := rm.configs[bucket]
	if !exists {
		return nil, fmt.Errorf("no replication configuration for bucket %s", bucket)
	}

	return config, nil
}

// DeleteReplicationConfiguration deletes replication configuration for a bucket.
func (rm *ReplicationManager) DeleteReplicationConfiguration(bucket string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	delete(rm.configs, bucket)
	return nil
}

// RegisterEndpoint registers a remote region endpoint.
func (rm *ReplicationManager) RegisterEndpoint(endpoint *RegionEndpoint) {
	rm.mu.Lock()
	rm.endpoints[endpoint.Region] = endpoint
	rm.mu.Unlock()
}

// validateConfiguration validates a replication configuration.
func (rm *ReplicationManager) validateConfiguration(config *ReplicationConfiguration) error {
	if config == nil {
		return fmt.Errorf("configuration is nil")
	}

	if len(config.Rules) == 0 {
		return fmt.Errorf("at least one rule is required")
	}

	// Check for unique IDs and priorities
	ids := make(map[string]bool)
	priorities := make(map[int]bool)

	for _, rule := range config.Rules {
		if rule.ID == "" {
			return fmt.Errorf("rule ID is required")
		}
		if ids[rule.ID] {
			return fmt.Errorf("duplicate rule ID: %s", rule.ID)
		}
		ids[rule.ID] = true

		if priorities[rule.Priority] {
			return fmt.Errorf("duplicate priority: %d", rule.Priority)
		}
		priorities[rule.Priority] = true

		if rule.Status != "Enabled" && rule.Status != "Disabled" {
			return fmt.Errorf("invalid rule status: %s", rule.Status)
		}

		if rule.Destination.Bucket == "" {
			return fmt.Errorf("destination bucket is required")
		}
	}

	return nil
}

// OnObjectCreated is called when a new object is created.
func (rm *ReplicationManager) OnObjectCreated(ctx context.Context, bucket, key string, info *S3ObjectInfo) error {
	rm.mu.RLock()
	config := rm.configs[bucket]
	rm.mu.RUnlock()

	if config == nil {
		return nil // No replication configured
	}

	// Find matching rules
	for _, rule := range config.Rules {
		if rule.Status != "Enabled" {
			continue
		}

		if rm.matchesFilter(&rule, key, info) {
			task := &ReplicationTask{
				ID:              uuid.New().String(),
				SourceBucket:    bucket,
				SourceKey:       key,
				SourceVersionID: info.VersionID,
				DestBucket:      rm.extractBucketName(rule.Destination.Bucket),
				DestKey:         key,
				DestRegion:      rm.extractRegion(rule.Destination.Bucket),
				Rule:            &rule,
				Status:          S3ReplicationStatusPending,
				CreatedAt:       time.Now(),
				Size:            info.Size,
				ETag:            info.ETag,
			}

			select {
			case rm.tasks <- task:
				atomic.AddInt64(&rm.metrics.pendingCount, 1)
			default:
				return fmt.Errorf("replication queue is full")
			}
		}
	}

	return nil
}

// OnObjectDeleted is called when an object is deleted.
func (rm *ReplicationManager) OnObjectDeleted(ctx context.Context, bucket, key, versionID string) error {
	rm.mu.RLock()
	config := rm.configs[bucket]
	rm.mu.RUnlock()

	if config == nil {
		return nil
	}

	for _, rule := range config.Rules {
		if rule.Status != "Enabled" {
			continue
		}

		// Check if delete marker replication is enabled
		if rule.DeleteMarkerReplication != nil && rule.DeleteMarkerReplication.Status == "Enabled" {
			if rm.matchesPrefixFilter(&rule, key) {
				// Queue delete replication
				task := &ReplicationTask{
					ID:              uuid.New().String(),
					SourceBucket:    bucket,
					SourceKey:       key,
					SourceVersionID: versionID,
					DestBucket:      rm.extractBucketName(rule.Destination.Bucket),
					DestKey:         key,
					DestRegion:      rm.extractRegion(rule.Destination.Bucket),
					Rule:            &rule,
					Status:          S3ReplicationStatusPending,
					CreatedAt:       time.Now(),
				}

				select {
				case rm.tasks <- task:
				default:
					return fmt.Errorf("replication queue is full")
				}
			}
		}
	}

	return nil
}

// matchesFilter checks if an object matches the rule filter.
func (rm *ReplicationManager) matchesFilter(rule *ReplicationRule, key string, info *S3ObjectInfo) bool {
	if rule.Filter == nil {
		return true
	}

	// Check prefix
	if rule.Filter.Prefix != "" && !strings.HasPrefix(key, rule.Filter.Prefix) {
		return false
	}

	// Check tag
	if rule.Filter.Tag != nil {
		tags, err := rm.storage.GetObjectTags(context.Background(), info.Key, key)
		if err != nil {
			return false
		}
		if tags[rule.Filter.Tag.Key] != rule.Filter.Tag.Value {
			return false
		}
	}

	// Check And conditions
	if rule.Filter.And != nil {
		if rule.Filter.And.Prefix != "" && !strings.HasPrefix(key, rule.Filter.And.Prefix) {
			return false
		}
		if len(rule.Filter.And.Tags) > 0 {
			tags, err := rm.storage.GetObjectTags(context.Background(), info.Key, key)
			if err != nil {
				return false
			}
			for _, tag := range rule.Filter.And.Tags {
				if tags[tag.Key] != tag.Value {
					return false
				}
			}
		}
	}

	return true
}

// matchesPrefixFilter checks if a key matches the prefix filter.
func (rm *ReplicationManager) matchesPrefixFilter(rule *ReplicationRule, key string) bool {
	if rule.Filter == nil {
		return true
	}
	if rule.Filter.Prefix != "" && !strings.HasPrefix(key, rule.Filter.Prefix) {
		return false
	}
	if rule.Filter.And != nil && rule.Filter.And.Prefix != "" {
		if !strings.HasPrefix(key, rule.Filter.And.Prefix) {
			return false
		}
	}
	return true
}

// extractBucketName extracts bucket name from ARN.
func (rm *ReplicationManager) extractBucketName(arn string) string {
	// Format: arn:aws:s3:::bucket-name
	parts := strings.Split(arn, ":::")
	if len(parts) == 2 {
		return parts[1]
	}
	return arn
}

// extractRegion extracts region from ARN.
func (rm *ReplicationManager) extractRegion(arn string) string {
	// Format: arn:aws:s3:region:account:bucket
	parts := strings.Split(arn, ":")
	if len(parts) >= 4 {
		return parts[3]
	}
	return "us-east-1"
}

// worker processes replication tasks.
func (rm *ReplicationManager) worker() {
	defer rm.wg.Done()

	for {
		select {
		case <-rm.ctx.Done():
			return
		case task := <-rm.tasks:
			atomic.AddInt32(&rm.activeWorkers, 1)
			rm.processTask(task)
			atomic.AddInt32(&rm.activeWorkers, -1)
			atomic.AddInt64(&rm.metrics.pendingCount, -1)
		}
	}
}

// processTask processes a single replication task.
func (rm *ReplicationManager) processTask(task *ReplicationTask) {
	start := time.Now()
	task.Attempts++
	task.LastAttempt = time.Now()

	ctx, cancel := context.WithTimeout(rm.ctx, 5*time.Minute)
	defer cancel()

	err := rm.replicateObject(ctx, task)
	if err != nil {
		task.Status = S3ReplicationStatusFailed
		task.Error = err.Error()
		atomic.AddInt64(&rm.metrics.objectsFailed, 1)

		// Retry logic
		if task.Attempts < 3 {
			time.Sleep(time.Duration(task.Attempts) * time.Second)
			select {
			case rm.tasks <- task:
				atomic.AddInt64(&rm.metrics.pendingCount, 1)
			default:
				// Queue full, log error
			}
		}
	} else {
		task.Status = S3ReplicationStatusCompleted
		atomic.AddInt64(&rm.metrics.objectsReplicated, 1)
		atomic.AddInt64(&rm.metrics.bytesReplicated, task.Size)

		rm.metrics.mu.Lock()
		rm.metrics.replicationLatency = append(rm.metrics.replicationLatency, time.Since(start))
		if len(rm.metrics.replicationLatency) > 1000 {
			rm.metrics.replicationLatency = rm.metrics.replicationLatency[500:]
		}
		rm.metrics.lastReplicationTime = time.Now()
		rm.metrics.mu.Unlock()
	}
}

// replicateObject replicates a single object to the destination.
func (rm *ReplicationManager) replicateObject(ctx context.Context, task *ReplicationTask) error {
	// Get source object
	var reader io.ReadCloser
	var info *S3ObjectInfo
	var err error

	if task.SourceVersionID != "" {
		reader, info, err = rm.storage.GetObjectVersion(ctx, task.SourceBucket, task.SourceKey, task.SourceVersionID)
	} else {
		reader, info, err = rm.storage.GetObject(ctx, task.SourceBucket, task.SourceKey)
	}
	if err != nil {
		return fmt.Errorf("failed to get source object: %w", err)
	}
	defer reader.Close()

	// Get destination endpoint
	rm.mu.RLock()
	endpoint := rm.endpoints[task.DestRegion]
	rm.mu.RUnlock()

	if endpoint == nil {
		return fmt.Errorf("no endpoint configured for region %s", task.DestRegion)
	}

	// Build destination options
	opts := &PutOptions{
		ContentType:  info.ContentType,
		Metadata:     info.Metadata,
		StorageClass: task.Rule.Destination.StorageClass,
	}

	if opts.StorageClass == "" {
		opts.StorageClass = info.StorageClass
	}

	// Replicate to remote endpoint
	err = rm.putObjectRemote(ctx, endpoint, task.DestBucket, task.DestKey, reader, info.Size, opts)
	if err != nil {
		return fmt.Errorf("failed to replicate object: %w", err)
	}

	return nil
}

// putObjectRemote puts an object to a remote endpoint.
func (rm *ReplicationManager) putObjectRemote(ctx context.Context, endpoint *RegionEndpoint, bucket, key string, reader io.Reader, size int64, opts *PutOptions) error {
	// Build URL
	scheme := "http"
	if endpoint.Secure {
		scheme = "https"
	}
	url := fmt.Sprintf("%s://%s/%s/%s", scheme, endpoint.Endpoint, bucket, key)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "PUT", url, reader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.ContentLength = size
	req.Header.Set("Content-Type", opts.ContentType)

	// Add metadata headers
	for k, v := range opts.Metadata {
		req.Header.Set("x-amz-meta-"+k, v)
	}

	if opts.StorageClass != "" {
		req.Header.Set("x-amz-storage-class", opts.StorageClass)
	}

	// Add replication status header
	req.Header.Set("x-amz-replication-status", string(S3ReplicationStatusReplica))

	// Sign request
	rm.signRequest(req, endpoint)

	// Execute request
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("put failed: %s - %s", resp.Status, string(body))
	}

	return nil
}

// signRequest signs an S3 request using AWS Signature V4.
func (rm *ReplicationManager) signRequest(req *http.Request, endpoint *RegionEndpoint) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("Host", endpoint.Endpoint)

	// Create canonical request
	canonicalHeaders := rm.getCanonicalHeaders(req)
	signedHeaders := rm.getSignedHeaders(req)
	payloadHash := "UNSIGNED-PAYLOAD"
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.Path,
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	// Create string to sign
	credentialScope := fmt.Sprintf("%s/%s/s3/aws4_request", dateStamp, endpoint.Region)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		rm.sha256Hash([]byte(canonicalRequest)),
	}, "\n")

	// Calculate signature
	signingKey := rm.getSignatureKey(endpoint.SecretKey, dateStamp, endpoint.Region, "s3")
	signature := hex.EncodeToString(rm.hmacSHA256(signingKey, []byte(stringToSign)))

	// Build authorization header
	authorization := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		endpoint.AccessKey,
		credentialScope,
		signedHeaders,
		signature,
	)

	req.Header.Set("Authorization", authorization)
}

func (rm *ReplicationManager) getCanonicalHeaders(req *http.Request) string {
	headers := make(map[string]string)
	for k := range req.Header {
		lowerKey := strings.ToLower(k)
		if strings.HasPrefix(lowerKey, "x-amz-") || lowerKey == "host" || lowerKey == "content-type" {
			headers[lowerKey] = strings.TrimSpace(req.Header.Get(k))
		}
	}

	var keys []string
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var canonical strings.Builder
	for _, k := range keys {
		canonical.WriteString(k)
		canonical.WriteString(":")
		canonical.WriteString(headers[k])
		canonical.WriteString("\n")
	}

	return canonical.String()
}

func (rm *ReplicationManager) getSignedHeaders(req *http.Request) string {
	var headers []string
	for k := range req.Header {
		lowerKey := strings.ToLower(k)
		if strings.HasPrefix(lowerKey, "x-amz-") || lowerKey == "host" || lowerKey == "content-type" {
			headers = append(headers, lowerKey)
		}
	}
	sort.Strings(headers)
	return strings.Join(headers, ";")
}

func (rm *ReplicationManager) sha256Hash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func (rm *ReplicationManager) hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func (rm *ReplicationManager) getSignatureKey(secretKey, dateStamp, region, service string) []byte {
	kDate := rm.hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := rm.hmacSHA256(kDate, []byte(region))
	kService := rm.hmacSHA256(kRegion, []byte(service))
	kSigning := rm.hmacSHA256(kService, []byte("aws4_request"))
	return kSigning
}

// GetMetrics returns current replication metrics.
func (rm *ReplicationManager) GetMetrics() *ReplicationMetricsSnapshot {
	rm.metrics.mu.Lock()
	defer rm.metrics.mu.Unlock()

	var avgLatency time.Duration
	if len(rm.metrics.replicationLatency) > 0 {
		var total time.Duration
		for _, l := range rm.metrics.replicationLatency {
			total += l
		}
		avgLatency = total / time.Duration(len(rm.metrics.replicationLatency))
	}

	return &ReplicationMetricsSnapshot{
		ObjectsReplicated:  rm.metrics.objectsReplicated,
		BytesReplicated:    rm.metrics.bytesReplicated,
		ObjectsFailed:      rm.metrics.objectsFailed,
		PendingCount:       rm.metrics.pendingCount,
		AverageLatency:     avgLatency,
		LastReplication:    rm.metrics.lastReplicationTime,
		ActiveWorkers:      int(atomic.LoadInt32(&rm.activeWorkers)),
	}
}

// ReplicationMetricsSnapshot contains a snapshot of replication metrics.
type ReplicationMetricsSnapshot struct {
	ObjectsReplicated int64
	BytesReplicated   int64
	ObjectsFailed     int64
	PendingCount      int64
	AverageLatency    time.Duration
	LastReplication   time.Time
	ActiveWorkers     int
}

// Stop stops the replication manager.
func (rm *ReplicationManager) Stop() {
	rm.cancel()
	rm.wg.Wait()
}

// ReplicationHandler handles HTTP requests for replication configuration.
type ReplicationHandler struct {
	manager *ReplicationManager
}

// NewReplicationHandler creates a new replication HTTP handler.
func NewReplicationHandler(manager *ReplicationManager) *ReplicationHandler {
	return &ReplicationHandler{manager: manager}
}

// HandleGetReplication handles GET bucket replication requests.
func (h *ReplicationHandler) HandleGetReplication(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")

	config, err := h.manager.GetReplicationConfiguration(bucket)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		xml.NewEncoder(w).Encode(map[string]string{
			"Code":    "ReplicationConfigurationNotFoundError",
			"Message": "The replication configuration was not found",
		})
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	xml.NewEncoder(w).Encode(config)
}

// HandlePutReplication handles PUT bucket replication requests.
func (h *ReplicationHandler) HandlePutReplication(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")

	var config ReplicationConfiguration
	if err := xml.NewDecoder(r.Body).Decode(&config); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		xml.NewEncoder(w).Encode(map[string]string{
			"Code":    "MalformedXML",
			"Message": "The XML you provided was not well-formed",
		})
		return
	}

	if err := h.manager.SetReplicationConfiguration(bucket, &config); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		xml.NewEncoder(w).Encode(map[string]string{
			"Code":    "InvalidRequest",
			"Message": err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
}

// HandleDeleteReplication handles DELETE bucket replication requests.
func (h *ReplicationHandler) HandleDeleteReplication(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")

	if err := h.manager.DeleteReplicationConfiguration(bucket); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleGetMetrics handles GET replication metrics requests.
func (h *ReplicationHandler) HandleGetMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := h.manager.GetMetrics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(metrics)
}

// ReplicateBatch processes a batch of existing objects for replication.
func (rm *ReplicationManager) ReplicateBatch(ctx context.Context, bucket string, objects []*S3ObjectInfo) error {
	rm.mu.RLock()
	config := rm.configs[bucket]
	rm.mu.RUnlock()

	if config == nil {
		return fmt.Errorf("no replication configuration for bucket %s", bucket)
	}

	for _, info := range objects {
		if err := rm.OnObjectCreated(ctx, bucket, info.Key, info); err != nil {
			return err
		}
	}

	return nil
}
