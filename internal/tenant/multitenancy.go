// Package tenant provides multi-tenancy support with namespace isolation.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Default quota constants.
const (
	defaultMaxStorageBytes      = 100 * 1024 * 1024 * 1024 * 1024 // 100 TB
	defaultMaxBuckets           = 100
	defaultMaxObjectsPerBucket  = 10000000                       // 10 million
	defaultMaxObjectSize        = 5 * 1024 * 1024 * 1024 * 1024  // 5 TB
	defaultMaxRequestsPerSecond = 10000
	defaultMaxConcurrentUploads = 100
	defaultMaxBandwidthBps      = 10 * 1024 * 1024 * 1024        // 10 Gbps
	defaultMaxUsers             = 100
	defaultMaxAccessKeys        = 10
	defaultQuotaWarningThreshold = 0.8
)

// Slug validation constants.
const (
	minSlugLength = 3
	maxSlugLength = 63
)

// Percentage multiplier for display.
const percentMultiplierTenant = 100

// TenantStatus represents the status of a tenant.
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusDeleted   TenantStatus = "deleted"
)

// Tenant represents a tenant in the system.
type Tenant struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Slug             string            `json:"slug"`
	Status           TenantStatus      `json:"status"`
	Namespace        string            `json:"namespace"`
	Quota            *TenantQuota      `json:"quota"`
	Settings         *TenantSettings   `json:"settings"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
	SuspendedAt      *time.Time        `json:"suspendedAt,omitempty"`
	SuspensionReason string            `json:"suspensionReason,omitempty"`
}

// TenantQuota defines resource limits for a tenant.
type TenantQuota struct {
	// Storage limits
	MaxStorageBytes     int64 `json:"maxStorageBytes"`     // Total storage limit
	MaxBuckets          int   `json:"maxBuckets"`          // Maximum number of buckets
	MaxObjectsPerBucket int64 `json:"maxObjectsPerBucket"` // Max objects per bucket
	MaxObjectSize       int64 `json:"maxObjectSize"`       // Max size per object

	// API limits
	MaxRequestsPerSecond int   `json:"maxRequestsPerSecond"` // Rate limit
	MaxConcurrentUploads int   `json:"maxConcurrentUploads"` // Concurrent upload limit
	MaxBandwidthBps      int64 `json:"maxBandwidthBps"`      // Bandwidth limit

	// User limits
	MaxUsers      int `json:"maxUsers"`      // Maximum users
	MaxAccessKeys int `json:"maxAccessKeys"` // Max access keys per user

	// Feature limits
	EnableVersioning  bool `json:"enableVersioning"`
	EnableReplication bool `json:"enableReplication"`
	EnableEncryption  bool `json:"enableEncryption"`
	EnableObjectLock  bool `json:"enableObjectLock"`
	EnableAIMl        bool `json:"enableAiMl"`
}

// TenantSettings contains tenant-specific settings.
type TenantSettings struct {
	DefaultRegion         string   `json:"defaultRegion"`
	CostCenter            string   `json:"costCenter,omitempty"`
	DefaultStorageClass   string   `json:"defaultStorageClass"`
	EncryptionKeyID       string   `json:"encryptionKeyId,omitempty"`
	BucketNamePrefix      string   `json:"bucketNamePrefix"`
	BillingEmail          string   `json:"billingEmail,omitempty"`
	WebhookURL            string   `json:"webhookUrl,omitempty"`
	AllowedStorageClasses []string `json:"allowedStorageClasses"`
	AllowedRegions        []string `json:"allowedRegions"`
	AllowedIPRanges       []string `json:"allowedIpRanges"`
	QuotaWarningThreshold float64  `json:"quotaWarningThreshold"`
	EnforceEncryption     bool     `json:"enforceEncryption"`
	NotifyOnQuotaWarning  bool     `json:"notifyOnQuotaWarning"`
	EnforceBucketPrefix   bool     `json:"enforceBucketPrefix"`
	RequireMFA            bool     `json:"requireMfa"`
}

// TenantUsage tracks resource usage for a tenant.
type TenantUsage struct {
	LastUpdated    time.Time `json:"lastUpdated"`
	TenantID       string    `json:"tenantId"`
	StorageBytes   int64     `json:"storageBytes"`
	ObjectCount    int64     `json:"objectCount"`
	BucketCount    int       `json:"bucketCount"`
	UserCount      int       `json:"userCount"`
	RequestsToday  int64     `json:"requestsToday"`
	BandwidthToday int64     `json:"bandwidthToday"`
}

// TenantManager manages multi-tenant operations.
type TenantManager struct {
	storage     TenantStorage
	tenants     map[string]*Tenant
	namespaces  map[string]string
	slugs       map[string]string
	usage       map[string]*TenantUsage
	namespaceRE *regexp.Regexp
	mu          sync.RWMutex
}

// TenantStorage interface for persisting tenant data.
type TenantStorage interface {
	SaveTenant(ctx context.Context, tenant *Tenant) error
	GetTenant(ctx context.Context, id string) (*Tenant, error)
	DeleteTenant(ctx context.Context, id string) error
	ListTenants(ctx context.Context) ([]*Tenant, error)
	SaveUsage(ctx context.Context, usage *TenantUsage) error
	GetUsage(ctx context.Context, tenantID string) (*TenantUsage, error)
}

// NamespaceContext contains tenant context for request processing.
type NamespaceContext struct {
	Quota      *TenantQuota
	Settings   *TenantSettings
	TenantID   string
	TenantName string
	Namespace  string
}

// contextKey is a custom type for context keys.
type contextKey string

const tenantContextKey contextKey = "tenant"

// DefaultQuota returns the default quota for new tenants.
func DefaultQuota() *TenantQuota {
	return &TenantQuota{
		MaxStorageBytes:      defaultMaxStorageBytes,
		MaxBuckets:           defaultMaxBuckets,
		MaxObjectsPerBucket:  defaultMaxObjectsPerBucket,
		MaxObjectSize:        defaultMaxObjectSize,
		MaxRequestsPerSecond: defaultMaxRequestsPerSecond,
		MaxConcurrentUploads: defaultMaxConcurrentUploads,
		MaxBandwidthBps:      defaultMaxBandwidthBps,
		MaxUsers:             defaultMaxUsers,
		MaxAccessKeys:        defaultMaxAccessKeys,
		EnableVersioning:     true,
		EnableReplication:    true,
		EnableEncryption:     true,
		EnableObjectLock:     true,
		EnableAIMl:           true,
	}
}

// DefaultSettings returns the default settings for new tenants.
func DefaultSettings() *TenantSettings {
	return &TenantSettings{
		DefaultStorageClass:   "STANDARD",
		AllowedStorageClasses: []string{"STANDARD", "REDUCED_REDUNDANCY", "GLACIER"},
		DefaultRegion:         "us-east-1",
		AllowedRegions:        []string{"us-east-1", "us-west-2", "eu-west-1"},
		RequireMFA:            false,
		EnforceEncryption:     false,
		EnforceBucketPrefix:   true,
		NotifyOnQuotaWarning:  true,
		QuotaWarningThreshold: defaultQuotaWarningThreshold,
	}
}

// NewTenantManager creates a new tenant manager.
func NewTenantManager(storage TenantStorage) (*TenantManager, error) {
	tm := &TenantManager{
		tenants:     make(map[string]*Tenant),
		namespaces:  make(map[string]string),
		slugs:       make(map[string]string),
		usage:       make(map[string]*TenantUsage),
		storage:     storage,
		namespaceRE: regexp.MustCompile(`^[a-z][a-z0-9-]*[a-z0-9]$`),
	}

	// Load existing tenants
	err := tm.loadTenants()
	if err != nil {
		return nil, fmt.Errorf("failed to load tenants: %w", err)
	}

	return tm, nil
}

// loadTenants loads tenants from storage.
func (tm *TenantManager) loadTenants() error {
	if tm.storage == nil {
		return nil
	}

	tenants, err := tm.storage.ListTenants(context.Background())
	if err != nil {
		return err
	}

	for _, t := range tenants {
		tm.tenants[t.ID] = t
		tm.namespaces[t.Namespace] = t.ID
		tm.slugs[t.Slug] = t.ID
	}

	return nil
}

// CreateTenant creates a new tenant.
func (tm *TenantManager) CreateTenant(ctx context.Context, name string, opts *CreateTenantOptions) (*Tenant, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Generate slug from name
	slug := tm.generateSlug(name)
	if _, exists := tm.slugs[slug]; exists {
		return nil, errors.New("tenant with similar name already exists")
	}

	// Generate namespace
	namespace := slug
	if opts != nil && opts.Namespace != "" {
		namespace = opts.Namespace
	}

	// Validate namespace
	if !tm.namespaceRE.MatchString(namespace) {
		return nil, errors.New("invalid namespace format: must be lowercase alphanumeric with hyphens")
	}

	if len(namespace) < 3 || len(namespace) > 63 {
		return nil, errors.New("namespace must be between 3 and 63 characters")
	}

	if _, exists := tm.namespaces[namespace]; exists {
		return nil, errors.New("namespace already in use")
	}

	tenant := &Tenant{
		ID:        uuid.New().String(),
		Name:      name,
		Slug:      slug,
		Status:    TenantStatusActive,
		Namespace: namespace,
		Quota:     DefaultQuota(),
		Settings:  DefaultSettings(),
		Metadata:  make(map[string]string),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Apply options
	if opts != nil {
		if opts.Quota != nil {
			tenant.Quota = opts.Quota
		}

		if opts.Settings != nil {
			tenant.Settings = opts.Settings
		}

		if opts.Metadata != nil {
			tenant.Metadata = opts.Metadata
		}
	}

	// Set bucket name prefix
	if tenant.Settings.EnforceBucketPrefix && tenant.Settings.BucketNamePrefix == "" {
		tenant.Settings.BucketNamePrefix = namespace + "-"
	}

	// Save to storage
	if tm.storage != nil {
		err := tm.storage.SaveTenant(ctx, tenant)
		if err != nil {
			return nil, fmt.Errorf("failed to save tenant: %w", err)
		}
	}

	// Initialize usage
	tm.usage[tenant.ID] = &TenantUsage{
		TenantID:    tenant.ID,
		LastUpdated: time.Now(),
	}

	// Add to indexes
	tm.tenants[tenant.ID] = tenant
	tm.namespaces[tenant.Namespace] = tenant.ID
	tm.slugs[tenant.Slug] = tenant.ID

	return tenant, nil
}

// CreateTenantOptions contains options for creating a tenant.
type CreateTenantOptions struct {
	Quota     *TenantQuota
	Settings  *TenantSettings
	Metadata  map[string]string
	Namespace string
}

// generateSlug generates a URL-safe slug from a name.
func (tm *TenantManager) generateSlug(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(slug, "")
	slug = regexp.MustCompile(`-+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	if len(slug) < minSlugLength {
		slug += "-tenant"
	}

	if len(slug) > maxSlugLength {
		slug = slug[:maxSlugLength]
	}

	return slug
}

// GetTenant returns a tenant by ID.
func (tm *TenantManager) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenant, exists := tm.tenants[id]
	if !exists {
		return nil, fmt.Errorf("tenant not found: %s", id)
	}

	return tenant, nil
}

// GetTenantByNamespace returns a tenant by namespace.
func (tm *TenantManager) GetTenantByNamespace(ctx context.Context, namespace string) (*Tenant, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenantID, exists := tm.namespaces[namespace]
	if !exists {
		return nil, fmt.Errorf("namespace not found: %s", namespace)
	}

	tenant, exists := tm.tenants[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant not found for namespace: %s", namespace)
	}

	return tenant, nil
}

// GetTenantBySlug returns a tenant by slug.
func (tm *TenantManager) GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenantID, exists := tm.slugs[slug]
	if !exists {
		return nil, fmt.Errorf("tenant not found: %s", slug)
	}

	return tm.tenants[tenantID], nil
}

// UpdateTenant updates a tenant.
func (tm *TenantManager) UpdateTenant(ctx context.Context, id string, updates *TenantUpdates) (*Tenant, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tenant, exists := tm.tenants[id]
	if !exists {
		return nil, fmt.Errorf("tenant not found: %s", id)
	}

	if tenant.Status == TenantStatusDeleted {
		return nil, errors.New("cannot update deleted tenant")
	}

	if updates.Name != "" && updates.Name != tenant.Name {
		tenant.Name = updates.Name
	}

	if updates.Quota != nil {
		tenant.Quota = updates.Quota
	}

	if updates.Settings != nil {
		tenant.Settings = updates.Settings
	}

	if updates.Metadata != nil {
		for k, v := range updates.Metadata {
			tenant.Metadata[k] = v
		}
	}

	tenant.UpdatedAt = time.Now()

	if tm.storage != nil {
		err := tm.storage.SaveTenant(ctx, tenant)
		if err != nil {
			return nil, fmt.Errorf("failed to save tenant: %w", err)
		}
	}

	return tenant, nil
}

// TenantUpdates contains fields that can be updated.
type TenantUpdates struct {
	Quota    *TenantQuota
	Settings *TenantSettings
	Metadata map[string]string
	Name     string
}

// SuspendTenant suspends a tenant.
func (tm *TenantManager) SuspendTenant(ctx context.Context, id, reason string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tenant, exists := tm.tenants[id]
	if !exists {
		return fmt.Errorf("tenant not found: %s", id)
	}

	if tenant.Status == TenantStatusDeleted {
		return errors.New("tenant is deleted")
	}

	now := time.Now()
	tenant.Status = TenantStatusSuspended
	tenant.SuspendedAt = &now
	tenant.SuspensionReason = reason
	tenant.UpdatedAt = now

	if tm.storage != nil {
		err := tm.storage.SaveTenant(ctx, tenant)
		if err != nil {
			return fmt.Errorf("failed to save tenant: %w", err)
		}
	}

	return nil
}

// ActivateTenant activates a suspended tenant.
func (tm *TenantManager) ActivateTenant(ctx context.Context, id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tenant, exists := tm.tenants[id]
	if !exists {
		return fmt.Errorf("tenant not found: %s", id)
	}

	if tenant.Status == TenantStatusDeleted {
		return errors.New("cannot activate deleted tenant")
	}

	tenant.Status = TenantStatusActive
	tenant.SuspendedAt = nil
	tenant.SuspensionReason = ""
	tenant.UpdatedAt = time.Now()

	if tm.storage != nil {
		err := tm.storage.SaveTenant(ctx, tenant)
		if err != nil {
			return fmt.Errorf("failed to save tenant: %w", err)
		}
	}

	return nil
}

// DeleteTenant marks a tenant as deleted.
func (tm *TenantManager) DeleteTenant(ctx context.Context, id string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tenant, exists := tm.tenants[id]
	if !exists {
		return fmt.Errorf("tenant not found: %s", id)
	}

	tenant.Status = TenantStatusDeleted
	tenant.UpdatedAt = time.Now()

	if tm.storage != nil {
		err := tm.storage.SaveTenant(ctx, tenant)
		if err != nil {
			return fmt.Errorf("failed to save tenant: %w", err)
		}
	}

	// Remove from indexes
	delete(tm.namespaces, tenant.Namespace)
	delete(tm.slugs, tenant.Slug)

	return nil
}

// ListTenants returns all tenants.
func (tm *TenantManager) ListTenants(ctx context.Context) []*Tenant {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenants := make([]*Tenant, 0, len(tm.tenants))
	for _, t := range tm.tenants {
		if t.Status != TenantStatusDeleted {
			tenants = append(tenants, t)
		}
	}

	return tenants
}

// GetUsage returns the current usage for a tenant.
func (tm *TenantManager) GetUsage(ctx context.Context, tenantID string) (*TenantUsage, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	usage, exists := tm.usage[tenantID]
	if !exists {
		return nil, fmt.Errorf("usage not found for tenant: %s", tenantID)
	}

	return usage, nil
}

// UpdateUsage updates usage statistics for a tenant.
func (tm *TenantManager) UpdateUsage(ctx context.Context, tenantID string, delta *UsageDelta) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	usage, exists := tm.usage[tenantID]
	if !exists {
		usage = &TenantUsage{TenantID: tenantID}
		tm.usage[tenantID] = usage
	}

	if delta.StorageBytes != 0 {
		usage.StorageBytes += delta.StorageBytes
		if usage.StorageBytes < 0 {
			usage.StorageBytes = 0
		}
	}

	if delta.ObjectCount != 0 {
		usage.ObjectCount += delta.ObjectCount
		if usage.ObjectCount < 0 {
			usage.ObjectCount = 0
		}
	}

	if delta.BucketCount != 0 {
		usage.BucketCount += delta.BucketCount
		if usage.BucketCount < 0 {
			usage.BucketCount = 0
		}
	}

	if delta.UserCount != 0 {
		usage.UserCount += delta.UserCount
		if usage.UserCount < 0 {
			usage.UserCount = 0
		}
	}

	if delta.Requests != 0 {
		usage.RequestsToday += delta.Requests
	}

	if delta.Bandwidth != 0 {
		usage.BandwidthToday += delta.Bandwidth
	}

	usage.LastUpdated = time.Now()

	// Check quota warnings
	tenant := tm.tenants[tenantID]
	if tenant != nil && tenant.Settings.NotifyOnQuotaWarning {
		tm.checkQuotaWarnings(tenant, usage)
	}

	if tm.storage != nil {
		err := tm.storage.SaveUsage(ctx, usage)
		if err != nil {
			return fmt.Errorf("failed to save usage: %w", err)
		}
	}

	return nil
}

// UsageDelta represents a change in usage.
type UsageDelta struct {
	StorageBytes int64
	ObjectCount  int64
	BucketCount  int
	UserCount    int
	Requests     int64
	Bandwidth    int64
}

// checkQuotaWarnings checks for quota warnings and sends notifications.
func (tm *TenantManager) checkQuotaWarnings(tenant *Tenant, usage *TenantUsage) {
	threshold := tenant.Settings.QuotaWarningThreshold

	// Check storage quota
	if tenant.Quota.MaxStorageBytes > 0 {
		storagePercent := float64(usage.StorageBytes) / float64(tenant.Quota.MaxStorageBytes)
		if storagePercent >= threshold {
			tm.sendQuotaWarning(tenant, "storage", storagePercent)
		}
	}

	// Check bucket quota
	if tenant.Quota.MaxBuckets > 0 {
		bucketPercent := float64(usage.BucketCount) / float64(tenant.Quota.MaxBuckets)
		if bucketPercent >= threshold {
			tm.sendQuotaWarning(tenant, "buckets", bucketPercent)
		}
	}

	// Check user quota
	if tenant.Quota.MaxUsers > 0 {
		userPercent := float64(usage.UserCount) / float64(tenant.Quota.MaxUsers)
		if userPercent >= threshold {
			tm.sendQuotaWarning(tenant, "users", userPercent)
		}
	}
}

// sendQuotaWarning sends a quota warning notification.
func (tm *TenantManager) sendQuotaWarning(tenant *Tenant, resource string, percent float64) {
	// Implementation would send webhook/email notification
	// For now, just log
	fmt.Printf("Quota warning: tenant %s is at %.1f%% of %s quota\n",
		tenant.Name, percent*percentMultiplierTenant, resource)
}

// CheckQuota checks if an operation would exceed quota.
func (tm *TenantManager) CheckQuota(ctx context.Context, tenantID string, check *QuotaCheck) error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenant, exists := tm.tenants[tenantID]
	if !exists {
		return fmt.Errorf("tenant not found: %s", tenantID)
	}

	if tenant.Status != TenantStatusActive {
		return errors.New("tenant is not active")
	}

	usage := tm.usage[tenantID]
	if usage == nil {
		usage = &TenantUsage{}
	}

	// Check storage quota
	if check.AdditionalBytes > 0 {
		newStorage := usage.StorageBytes + check.AdditionalBytes
		if tenant.Quota.MaxStorageBytes > 0 && newStorage > tenant.Quota.MaxStorageBytes {
			return &QuotaExceededError{
				Resource:  "storage",
				Limit:     tenant.Quota.MaxStorageBytes,
				Current:   usage.StorageBytes,
				Requested: check.AdditionalBytes,
			}
		}
	}

	// Check object size
	if check.ObjectSize > 0 {
		if tenant.Quota.MaxObjectSize > 0 && check.ObjectSize > tenant.Quota.MaxObjectSize {
			return &QuotaExceededError{
				Resource:  "object_size",
				Limit:     tenant.Quota.MaxObjectSize,
				Requested: check.ObjectSize,
			}
		}
	}

	// Check bucket quota
	if check.NewBucket {
		if tenant.Quota.MaxBuckets > 0 && usage.BucketCount >= tenant.Quota.MaxBuckets {
			return &QuotaExceededError{
				Resource: "buckets",
				Limit:    int64(tenant.Quota.MaxBuckets),
				Current:  int64(usage.BucketCount),
			}
		}
	}

	return nil
}

// QuotaCheck represents a quota check request.
type QuotaCheck struct {
	AdditionalBytes int64
	ObjectSize      int64
	NewBucket       bool
	NewUser         bool
}

// QuotaExceededError is returned when quota is exceeded.
type QuotaExceededError struct {
	Resource  string
	Limit     int64
	Current   int64
	Requested int64
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("quota exceeded for %s: limit=%d, current=%d, requested=%d",
		e.Resource, e.Limit, e.Current, e.Requested)
}

// IsQuotaExceeded checks if an error is a quota exceeded error.
func IsQuotaExceeded(err error) bool {
	_, ok := err.(*QuotaExceededError)
	return ok
}

// ValidateBucketName validates a bucket name for a tenant.
func (tm *TenantManager) ValidateBucketName(ctx context.Context, tenantID, bucketName string) error {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenant, exists := tm.tenants[tenantID]
	if !exists {
		return fmt.Errorf("tenant not found: %s", tenantID)
	}

	// Check bucket name prefix requirement
	if tenant.Settings.EnforceBucketPrefix {
		if !strings.HasPrefix(bucketName, tenant.Settings.BucketNamePrefix) {
			return fmt.Errorf("bucket name must start with %s", tenant.Settings.BucketNamePrefix)
		}
	}

	return nil
}

// TransformBucketName transforms a bucket name with tenant namespace.
func (tm *TenantManager) TransformBucketName(ctx context.Context, tenantID, bucketName string, addPrefix bool) (string, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenant, exists := tm.tenants[tenantID]
	if !exists {
		return "", fmt.Errorf("tenant not found: %s", tenantID)
	}

	if addPrefix && tenant.Settings.EnforceBucketPrefix {
		if !strings.HasPrefix(bucketName, tenant.Settings.BucketNamePrefix) {
			return tenant.Settings.BucketNamePrefix + bucketName, nil
		}
	}

	return bucketName, nil
}

// GetNamespaceContext returns the tenant context for a request.
func (tm *TenantManager) GetNamespaceContext(ctx context.Context, tenantID string) (*NamespaceContext, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tenant, exists := tm.tenants[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant not found: %s", tenantID)
	}

	if tenant.Status != TenantStatusActive {
		return nil, fmt.Errorf("tenant is not active: %s", tenant.Status)
	}

	return &NamespaceContext{
		TenantID:   tenant.ID,
		TenantName: tenant.Name,
		Namespace:  tenant.Namespace,
		Quota:      tenant.Quota,
		Settings:   tenant.Settings,
	}, nil
}

// WithTenantContext adds tenant context to a context.
func WithTenantContext(ctx context.Context, nc *NamespaceContext) context.Context {
	return context.WithValue(ctx, tenantContextKey, nc)
}

// GetTenantContext retrieves tenant context from a context.
func GetTenantContext(ctx context.Context) *NamespaceContext {
	nc, _ := ctx.Value(tenantContextKey).(*NamespaceContext)
	return nc
}

// IsolationPolicy defines how tenants are isolated.
type IsolationPolicy struct {
	// NetworkIsolation enables network-level isolation
	NetworkIsolation bool

	// StorageIsolation enables storage-level isolation
	StorageIsolation bool

	// EncryptionIsolation uses per-tenant encryption keys
	EncryptionIsolation bool

	// ResourceIsolation enables CPU/memory isolation
	ResourceIsolation bool
}

// TenantMiddleware provides HTTP middleware for multi-tenant requests.
type TenantMiddleware struct {
	manager *TenantManager
}

// NewTenantMiddleware creates a new tenant middleware.
func NewTenantMiddleware(manager *TenantManager) *TenantMiddleware {
	return &TenantMiddleware{manager: manager}
}

// ExtractTenantFromHeader extracts tenant from request headers.
func (m *TenantMiddleware) ExtractTenantFromHeader(header string, value string) (*Tenant, error) {
	var (
		tenant *Tenant
		err    error
	)

	switch header {
	case "X-Tenant-ID":
		tenant, err = m.manager.GetTenant(context.Background(), value)
	case "X-Tenant-Namespace":
		tenant, err = m.manager.GetTenantByNamespace(context.Background(), value)
	case "X-Tenant-Slug":
		tenant, err = m.manager.GetTenantBySlug(context.Background(), value)
	default:
		return nil, fmt.Errorf("unsupported tenant header: %s", header)
	}

	if err != nil {
		return nil, err
	}

	if tenant.Status != TenantStatusActive {
		return nil, errors.New("tenant is not active")
	}

	return tenant, nil
}

// ExtractTenantFromBucketName extracts tenant from bucket name prefix.
func (m *TenantMiddleware) ExtractTenantFromBucketName(bucketName string) (*Tenant, error) {
	for _, tenant := range m.manager.tenants {
		if tenant.Settings.EnforceBucketPrefix {
			if strings.HasPrefix(bucketName, tenant.Settings.BucketNamePrefix) {
				return tenant, nil
			}
		}
	}

	return nil, fmt.Errorf("cannot determine tenant from bucket name: %s", bucketName)
}

// ResetDailyUsage resets daily usage counters for all tenants.
func (tm *TenantManager) ResetDailyUsage(ctx context.Context) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for _, usage := range tm.usage {
		usage.RequestsToday = 0
		usage.BandwidthToday = 0
	}
}
