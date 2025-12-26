package site

import (
	"errors"
	"net/url"
	"regexp"
	"time"
)

// Site represents a remote site in the replication cluster
type Site struct {
	// Name is the unique name for this site
	Name string `json:"name" yaml:"name"`
	// Endpoint is the S3-compatible endpoint URL
	Endpoint string `json:"endpoint" yaml:"endpoint"`
	// AccessKey for authentication
	AccessKey string `json:"accessKey" yaml:"accessKey"`
	// SecretKey for authentication
	SecretKey string `json:"secretKey" yaml:"secretKey"`
	// Region of the site
	Region string `json:"region,omitempty" yaml:"region,omitempty"`
	// UseSSL enables HTTPS
	UseSSL bool `json:"useSSL" yaml:"useSSL"`
	// IsLocal indicates if this is the local site
	IsLocal bool `json:"isLocal" yaml:"isLocal"`
	// Status is the current connection status
	Status SiteStatus `json:"status" yaml:"status"`
	// LastSeen is when the site was last reachable
	LastSeen time.Time `json:"lastSeen,omitempty" yaml:"lastSeen,omitempty"`
}

// SiteStatus represents the status of a site
type SiteStatus string

const (
	// SiteStatusOnline indicates the site is reachable
	SiteStatusOnline SiteStatus = "online"
	// SiteStatusOffline indicates the site is unreachable
	SiteStatusOffline SiteStatus = "offline"
	// SiteStatusSyncing indicates the site is syncing
	SiteStatusSyncing SiteStatus = "syncing"
	// SiteStatusError indicates an error with the site
	SiteStatusError SiteStatus = "error"
)

// Config holds site replication configuration
type Config struct {
	// Sites is the list of all sites in the replication cluster
	Sites []Site `json:"sites" yaml:"sites"`
	// SyncInterval is how often to check for sync
	SyncInterval time.Duration `json:"syncInterval" yaml:"syncInterval"`
	// HealthCheckInterval is how often to check site health
	HealthCheckInterval time.Duration `json:"healthCheckInterval" yaml:"healthCheckInterval"`
	// ConflictResolution determines how conflicts are resolved
	ConflictResolution ConflictResolution `json:"conflictResolution" yaml:"conflictResolution"`
	// SyncBuckets lists buckets to sync (empty = all buckets)
	SyncBuckets []string `json:"syncBuckets,omitempty" yaml:"syncBuckets,omitempty"`
	// SyncIAM enables IAM policy synchronization
	SyncIAM bool `json:"syncIAM" yaml:"syncIAM"`
	// SyncBucketConfig enables bucket configuration sync
	SyncBucketConfig bool `json:"syncBucketConfig" yaml:"syncBucketConfig"`
}

// ConflictResolution determines how conflicts are resolved
type ConflictResolution string

const (
	// ConflictLastWriteWins uses timestamp to resolve conflicts
	ConflictLastWriteWins ConflictResolution = "last-write-wins"
	// ConflictLocalWins always prefers local version
	ConflictLocalWins ConflictResolution = "local-wins"
	// ConflictRemoteWins always prefers remote version
	ConflictRemoteWins ConflictResolution = "remote-wins"
)

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		Sites:               []Site{},
		SyncInterval:        time.Minute,
		HealthCheckInterval: 30 * time.Second,
		ConflictResolution:  ConflictLastWriteWins,
		SyncIAM:             true,
		SyncBucketConfig:    true,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if len(c.Sites) == 0 {
		return errors.New("at least one site is required")
	}

	names := make(map[string]bool)
	localCount := 0

	for _, site := range c.Sites {
		if err := site.Validate(); err != nil {
			return err
		}

		if names[site.Name] {
			return errors.New("duplicate site name: " + site.Name)
		}
		names[site.Name] = true

		if site.IsLocal {
			localCount++
		}
	}

	if localCount != 1 {
		return errors.New("exactly one site must be marked as local")
	}

	if c.SyncInterval < time.Second {
		return errors.New("sync interval must be at least 1 second")
	}

	if c.HealthCheckInterval < time.Second {
		return errors.New("health check interval must be at least 1 second")
	}

	return nil
}

// Validate checks if the site configuration is valid
func (s *Site) Validate() error {
	if s.Name == "" {
		return errors.New("site name is required")
	}

	// Name must be alphanumeric with dashes and underscores
	nameRegex := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	if !nameRegex.MatchString(s.Name) {
		return errors.New("site name must be alphanumeric with dashes and underscores")
	}

	if s.Endpoint == "" {
		return errors.New("site endpoint is required")
	}

	// Validate endpoint URL
	if _, err := url.Parse(s.Endpoint); err != nil {
		return errors.New("invalid endpoint URL: " + err.Error())
	}

	if !s.IsLocal {
		if s.AccessKey == "" || s.SecretKey == "" {
			return errors.New("credentials required for remote sites")
		}
	}

	return nil
}

// GetLocalSite returns the local site from the config
func (c *Config) GetLocalSite() *Site {
	for i := range c.Sites {
		if c.Sites[i].IsLocal {
			return &c.Sites[i]
		}
	}
	return nil
}

// GetRemoteSites returns all remote sites
func (c *Config) GetRemoteSites() []Site {
	var remote []Site
	for _, site := range c.Sites {
		if !site.IsLocal {
			remote = append(remote, site)
		}
	}
	return remote
}

// GetSite returns a site by name
func (c *Config) GetSite(name string) *Site {
	for i := range c.Sites {
		if c.Sites[i].Name == name {
			return &c.Sites[i]
		}
	}
	return nil
}

// SyncState holds the current synchronization state
type SyncState struct {
	// LastSync is when sync last completed
	LastSync time.Time `json:"lastSync"`
	// SyncInProgress indicates if sync is currently running
	SyncInProgress bool `json:"syncInProgress"`
	// LastError is the last sync error
	LastError string `json:"lastError,omitempty"`
	// PendingObjects is the number of objects pending sync
	PendingObjects int64 `json:"pendingObjects"`
	// SyncedObjects is the total objects synced
	SyncedObjects int64 `json:"syncedObjects"`
	// SyncedBytes is the total bytes synced
	SyncedBytes int64 `json:"syncedBytes"`
}

// VectorClock tracks causality for conflict resolution
type VectorClock struct {
	// Clocks maps site name to logical clock value
	Clocks map[string]int64 `json:"clocks"`
}

// NewVectorClock creates a new vector clock
func NewVectorClock() *VectorClock {
	return &VectorClock{
		Clocks: make(map[string]int64),
	}
}

// Increment increments the clock for a site
func (v *VectorClock) Increment(site string) {
	v.Clocks[site]++
}

// Get returns the clock value for a site
func (v *VectorClock) Get(site string) int64 {
	return v.Clocks[site]
}

// Merge merges another vector clock into this one
func (v *VectorClock) Merge(other *VectorClock) {
	for site, clock := range other.Clocks {
		if v.Clocks[site] < clock {
			v.Clocks[site] = clock
		}
	}
}

// Compare compares two vector clocks
// Returns -1 if v < other, 0 if concurrent, 1 if v > other
func (v *VectorClock) Compare(other *VectorClock) int {
	vGreater := false
	otherGreater := false

	// Check all clocks in v
	for site, clock := range v.Clocks {
		otherClock := other.Clocks[site]
		if clock > otherClock {
			vGreater = true
		} else if clock < otherClock {
			otherGreater = true
		}
	}

	// Check clocks in other that aren't in v
	for site, clock := range other.Clocks {
		if _, ok := v.Clocks[site]; !ok {
			if clock > 0 {
				otherGreater = true
			}
		}
	}

	if vGreater && !otherGreater {
		return 1
	}
	if otherGreater && !vGreater {
		return -1
	}
	return 0 // Concurrent
}

// Copy creates a copy of the vector clock
func (v *VectorClock) Copy() *VectorClock {
	copy := NewVectorClock()
	for site, clock := range v.Clocks {
		copy.Clocks[site] = clock
	}
	return copy
}

// ObjectVersion tracks version information for an object
type ObjectVersion struct {
	// Key is the object key
	Key string `json:"key"`
	// VersionID is the S3 version ID
	VersionID string `json:"versionId,omitempty"`
	// ETag is the object ETag
	ETag string `json:"etag"`
	// Size is the object size
	Size int64 `json:"size"`
	// LastModified is when the object was last modified
	LastModified time.Time `json:"lastModified"`
	// Site is which site this version is from
	Site string `json:"site"`
	// VectorClock is the causal order
	VectorClock *VectorClock `json:"vectorClock"`
	// IsDeleteMarker indicates if this is a delete marker
	IsDeleteMarker bool `json:"isDeleteMarker"`
}

// IAMSync holds IAM synchronization state
type IAMSync struct {
	// Users to sync
	Users []string `json:"users,omitempty"`
	// Groups to sync
	Groups []string `json:"groups,omitempty"`
	// Policies to sync
	Policies []string `json:"policies,omitempty"`
	// LastSync is when IAM was last synced
	LastSync time.Time `json:"lastSync"`
}

// BucketSync holds bucket configuration sync state
type BucketSync struct {
	// Bucket name
	Bucket string `json:"bucket"`
	// LastSync is when the bucket config was last synced
	LastSync time.Time `json:"lastSync"`
	// SyncVersioning enables versioning sync
	SyncVersioning bool `json:"syncVersioning"`
	// SyncLifecycle enables lifecycle sync
	SyncLifecycle bool `json:"syncLifecycle"`
	// SyncNotifications enables notification sync
	SyncNotifications bool `json:"syncNotifications"`
	// SyncPolicy enables policy sync
	SyncPolicy bool `json:"syncPolicy"`
}
