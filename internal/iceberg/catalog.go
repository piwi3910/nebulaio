// Package iceberg implements Apache Iceberg REST Catalog API
// This provides native table format support for data lakehouse use cases
package iceberg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Iceberg Catalog Errors
var (
	ErrNamespaceNotFound     = errors.New("namespace not found")
	ErrNamespaceAlreadyExists = errors.New("namespace already exists")
	ErrTableNotFound         = errors.New("table not found")
	ErrTableAlreadyExists    = errors.New("table already exists")
	ErrInvalidSchema         = errors.New("invalid schema")
	ErrCommitConflict        = errors.New("commit conflict: table was modified")
	ErrViewNotFound          = errors.New("view not found")
	ErrRequirementFailed     = errors.New("requirement failed")
)

// Catalog implements the Iceberg REST Catalog API
type Catalog struct {
	config     *CatalogConfig
	store      MetadataStore
	warehouse  WarehouseStore
	namespaces sync.Map // map[string]*Namespace
	tables     sync.Map // map[string]*Table
	views      sync.Map // map[string]*View
	metrics    *CatalogMetrics
}

// CatalogConfig configures the Iceberg catalog
type CatalogConfig struct {
	// CatalogName is the name of this catalog
	CatalogName string

	// WarehouseLocation is the default warehouse location
	WarehouseLocation string

	// DefaultNamespace is the default namespace for operations
	DefaultNamespace string

	// S3Endpoint for object storage
	S3Endpoint string

	// S3Region for object storage
	S3Region string

	// EnableCaching enables metadata caching
	EnableCaching bool

	// CacheTTL is the cache time-to-live
	CacheTTL time.Duration
}

// DefaultCatalogConfig returns sensible defaults
func DefaultCatalogConfig() *CatalogConfig {
	return &CatalogConfig{
		CatalogName:       "nebulaio",
		WarehouseLocation: "s3://warehouse/",
		DefaultNamespace:  "default",
		EnableCaching:     true,
		CacheTTL:          5 * time.Minute,
	}
}

// MetadataStore interface for catalog metadata persistence
type MetadataStore interface {
	GetNamespace(ctx context.Context, name string) (*Namespace, error)
	PutNamespace(ctx context.Context, ns *Namespace) error
	DeleteNamespace(ctx context.Context, name string) error
	ListNamespaces(ctx context.Context, parent string) ([]*Namespace, error)

	GetTable(ctx context.Context, namespace, name string) (*Table, error)
	PutTable(ctx context.Context, table *Table) error
	DeleteTable(ctx context.Context, namespace, name string) error
	ListTables(ctx context.Context, namespace string) ([]*Table, error)

	GetView(ctx context.Context, namespace, name string) (*View, error)
	PutView(ctx context.Context, view *View) error
	DeleteView(ctx context.Context, namespace, name string) error
	ListViews(ctx context.Context, namespace string) ([]*View, error)
}

// WarehouseStore interface for data file storage
type WarehouseStore interface {
	WriteMetadata(ctx context.Context, path string, data []byte) error
	ReadMetadata(ctx context.Context, path string) ([]byte, error)
	DeleteMetadata(ctx context.Context, path string) error
	ListFiles(ctx context.Context, prefix string) ([]string, error)
}

// Namespace represents an Iceberg namespace (database)
type Namespace struct {
	Name       []string          `json:"namespace"`
	Properties map[string]string `json:"properties,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// NamespaceName returns the full namespace name
func (n *Namespace) NamespaceName() string {
	return strings.Join(n.Name, ".")
}

// Table represents an Iceberg table
type Table struct {
	Identifier      TableIdentifier   `json:"identifier"`
	Metadata        *TableMetadata    `json:"metadata"`
	MetadataLocation string           `json:"metadata-location"`
	Config          map[string]string `json:"config,omitempty"`
}

// TableIdentifier identifies a table
type TableIdentifier struct {
	Namespace []string `json:"namespace"`
	Name      string   `json:"name"`
}

// FullName returns the full table name
func (t *TableIdentifier) FullName() string {
	return strings.Join(t.Namespace, ".") + "." + t.Name
}

// TableMetadata contains Iceberg table metadata
type TableMetadata struct {
	FormatVersion       int                    `json:"format-version"`
	TableUUID           string                 `json:"table-uuid"`
	Location            string                 `json:"location"`
	LastUpdatedMs       int64                  `json:"last-updated-ms"`
	LastColumnID        int                    `json:"last-column-id"`
	Schema              *Schema                `json:"schema"`
	Schemas             []*Schema              `json:"schemas,omitempty"`
	CurrentSchemaID     int                    `json:"current-schema-id"`
	PartitionSpecs      []*PartitionSpec       `json:"partition-specs"`
	DefaultSpecID       int                    `json:"default-spec-id"`
	LastPartitionID     int                    `json:"last-partition-id"`
	Properties          map[string]string      `json:"properties,omitempty"`
	CurrentSnapshotID   *int64                 `json:"current-snapshot-id,omitempty"`
	Snapshots           []*Snapshot            `json:"snapshots,omitempty"`
	SnapshotLog         []*SnapshotLogEntry    `json:"snapshot-log,omitempty"`
	MetadataLog         []*MetadataLogEntry    `json:"metadata-log,omitempty"`
	SortOrders          []*SortOrder           `json:"sort-orders,omitempty"`
	DefaultSortOrderID  int                    `json:"default-sort-order-id"`
	Refs                map[string]*SnapshotRef `json:"refs,omitempty"`
	StatisticsFiles     []*StatisticsFile      `json:"statistics,omitempty"`
	PartitionStatistics []*PartitionStatistics `json:"partition-statistics,omitempty"`
}

// Schema represents an Iceberg schema
type Schema struct {
	SchemaID         int      `json:"schema-id"`
	IdentifierFieldIDs []int  `json:"identifier-field-ids,omitempty"`
	Fields           []*Field `json:"fields"`
}

// Field represents a schema field
type Field struct {
	ID             int         `json:"id"`
	Name           string      `json:"name"`
	Required       bool        `json:"required"`
	Type           interface{} `json:"type"` // Can be string or nested type
	Doc            string      `json:"doc,omitempty"`
	InitialDefault interface{} `json:"initial-default,omitempty"`
	WriteDefault   interface{} `json:"write-default,omitempty"`
}

// PartitionSpec defines table partitioning
type PartitionSpec struct {
	SpecID int              `json:"spec-id"`
	Fields []*PartitionField `json:"fields"`
}

// PartitionField defines a partition field
type PartitionField struct {
	SourceID  int    `json:"source-id"`
	FieldID   int    `json:"field-id"`
	Name      string `json:"name"`
	Transform string `json:"transform"`
}

// Snapshot represents a table snapshot
type Snapshot struct {
	SnapshotID       int64             `json:"snapshot-id"`
	ParentSnapshotID *int64            `json:"parent-snapshot-id,omitempty"`
	SequenceNumber   int64             `json:"sequence-number"`
	TimestampMs      int64             `json:"timestamp-ms"`
	ManifestList     string            `json:"manifest-list"`
	Summary          map[string]string `json:"summary"`
	SchemaID         *int              `json:"schema-id,omitempty"`
}

// SnapshotLogEntry records snapshot history
type SnapshotLogEntry struct {
	TimestampMs int64 `json:"timestamp-ms"`
	SnapshotID  int64 `json:"snapshot-id"`
}

// MetadataLogEntry records metadata file history
type MetadataLogEntry struct {
	TimestampMs      int64  `json:"timestamp-ms"`
	MetadataFile     string `json:"metadata-file"`
}

// SortOrder defines table sort order
type SortOrder struct {
	OrderID int          `json:"order-id"`
	Fields  []*SortField `json:"fields"`
}

// SortField defines a sort field
type SortField struct {
	SourceID      int    `json:"source-id"`
	Transform     string `json:"transform"`
	Direction     string `json:"direction"`
	NullOrder     string `json:"null-order"`
}

// SnapshotRef is a named reference to a snapshot
type SnapshotRef struct {
	SnapshotID          int64  `json:"snapshot-id"`
	Type                string `json:"type"` // "branch" or "tag"
	MaxRefAgeMs         *int64 `json:"max-ref-age-ms,omitempty"`
	MaxSnapshotAgeMs    *int64 `json:"max-snapshot-age-ms,omitempty"`
	MinSnapshotsToKeep  *int   `json:"min-snapshots-to-keep,omitempty"`
}

// StatisticsFile contains table statistics
type StatisticsFile struct {
	SnapshotID      int64  `json:"snapshot-id"`
	StatisticsPath  string `json:"statistics-path"`
	FileSizeInBytes int64  `json:"file-size-in-bytes"`
	FileFooterSizeInBytes int64 `json:"file-footer-size-in-bytes"`
	BlobMetadata    []BlobMetadata `json:"blob-metadata"`
}

// BlobMetadata for statistics
type BlobMetadata struct {
	Type       string            `json:"type"`
	SnapshotID int64             `json:"snapshot-id"`
	SequenceNumber int64         `json:"sequence-number"`
	Fields     []int             `json:"fields"`
	Properties map[string]string `json:"properties,omitempty"`
}

// PartitionStatistics contains partition-level statistics
type PartitionStatistics struct {
	SnapshotID         int64  `json:"snapshot-id"`
	StatisticsPath     string `json:"statistics-path"`
	FileSizeInBytes    int64  `json:"file-size-in-bytes"`
}

// View represents an Iceberg view
type View struct {
	Identifier       TableIdentifier `json:"identifier"`
	ViewMetadata     *ViewMetadata   `json:"metadata"`
	MetadataLocation string          `json:"metadata-location"`
}

// ViewMetadata contains Iceberg view metadata
type ViewMetadata struct {
	FormatVersion   int                    `json:"format-version"`
	ViewUUID        string                 `json:"view-uuid"`
	Location        string                 `json:"location"`
	CurrentVersionID int                   `json:"current-version-id"`
	Versions        []*ViewVersion         `json:"versions"`
	VersionLog      []*ViewVersionLog      `json:"version-log"`
	Schemas         []*Schema              `json:"schemas"`
	Properties      map[string]string      `json:"properties,omitempty"`
}

// ViewVersion represents a view version
type ViewVersion struct {
	VersionID         int               `json:"version-id"`
	SchemaID          int               `json:"schema-id"`
	TimestampMs       int64             `json:"timestamp-ms"`
	Summary           map[string]string `json:"summary"`
	Representations   []*ViewRepresentation `json:"representations"`
	DefaultCatalog    string            `json:"default-catalog,omitempty"`
	DefaultNamespace  []string          `json:"default-namespace,omitempty"`
}

// ViewRepresentation is a SQL representation of a view
type ViewRepresentation struct {
	Type    string `json:"type"` // "sql"
	SQL     string `json:"sql"`
	Dialect string `json:"dialect"`
}

// ViewVersionLog records view version history
type ViewVersionLog struct {
	TimestampMs int64 `json:"timestamp-ms"`
	VersionID   int   `json:"version-id"`
}

// CatalogMetrics tracks catalog performance
type CatalogMetrics struct {
	mu                    sync.RWMutex
	NamespacesCreated     int64
	NamespacesDeleted     int64
	TablesCreated         int64
	TablesUpdated         int64
	TablesDeleted         int64
	SnapshotsCreated      int64
	CommitsSucceeded      int64
	CommitsFailed         int64
	CommitConflicts       int64
	ViewsCreated          int64
	ViewsUpdated          int64
	CacheHits             int64
	CacheMisses           int64
}

// NewCatalog creates a new Iceberg catalog
func NewCatalog(store MetadataStore, warehouse WarehouseStore, config *CatalogConfig) *Catalog {
	if config == nil {
		config = DefaultCatalogConfig()
	}

	return &Catalog{
		config:    config,
		store:     store,
		warehouse: warehouse,
		metrics:   &CatalogMetrics{},
	}
}

// GetConfig returns the catalog configuration
func (c *Catalog) GetConfig(ctx context.Context) (*CatalogConfigResponse, error) {
	return &CatalogConfigResponse{
		Defaults:  map[string]string{
			"warehouse": c.config.WarehouseLocation,
		},
		Overrides: map[string]string{},
	}, nil
}

// CatalogConfigResponse is the response for config endpoint
type CatalogConfigResponse struct {
	Defaults  map[string]string `json:"defaults"`
	Overrides map[string]string `json:"overrides"`
}

// CreateNamespace creates a new namespace
func (c *Catalog) CreateNamespace(ctx context.Context, namespace []string, properties map[string]string) (*Namespace, error) {
	name := strings.Join(namespace, ".")

	// Check if exists
	if _, ok := c.namespaces.Load(name); ok {
		return nil, ErrNamespaceAlreadyExists
	}

	ns := &Namespace{
		Name:       namespace,
		Properties: properties,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := c.store.PutNamespace(ctx, ns); err != nil {
		return nil, err
	}

	c.namespaces.Store(name, ns)
	atomic.AddInt64(&c.metrics.NamespacesCreated, 1)

	return ns, nil
}

// GetNamespace retrieves a namespace
func (c *Catalog) GetNamespace(ctx context.Context, namespace []string) (*Namespace, error) {
	name := strings.Join(namespace, ".")

	// Check cache
	if val, ok := c.namespaces.Load(name); ok {
		atomic.AddInt64(&c.metrics.CacheHits, 1)
		return val.(*Namespace), nil
	}

	atomic.AddInt64(&c.metrics.CacheMisses, 1)

	ns, err := c.store.GetNamespace(ctx, name)
	if err != nil {
		return nil, ErrNamespaceNotFound
	}

	c.namespaces.Store(name, ns)
	return ns, nil
}

// ListNamespaces lists all namespaces
func (c *Catalog) ListNamespaces(ctx context.Context, parent []string) (*NamespaceListResponse, error) {
	parentName := ""
	if len(parent) > 0 {
		parentName = strings.Join(parent, ".")
	}

	namespaces, err := c.store.ListNamespaces(ctx, parentName)
	if err != nil {
		return nil, err
	}

	result := &NamespaceListResponse{
		Namespaces: make([][]string, 0, len(namespaces)),
	}

	for _, ns := range namespaces {
		result.Namespaces = append(result.Namespaces, ns.Name)
	}

	return result, nil
}

// NamespaceListResponse is the response for listing namespaces
type NamespaceListResponse struct {
	Namespaces [][]string `json:"namespaces"`
	NextPageToken string  `json:"next-page-token,omitempty"`
}

// UpdateNamespace updates namespace properties
func (c *Catalog) UpdateNamespace(ctx context.Context, namespace []string, removals []string, updates map[string]string) (*Namespace, error) {
	ns, err := c.GetNamespace(ctx, namespace)
	if err != nil {
		return nil, err
	}

	if ns.Properties == nil {
		ns.Properties = make(map[string]string)
	}

	// Remove properties
	for _, key := range removals {
		delete(ns.Properties, key)
	}

	// Update properties
	for key, value := range updates {
		ns.Properties[key] = value
	}

	ns.UpdatedAt = time.Now()

	if err := c.store.PutNamespace(ctx, ns); err != nil {
		return nil, err
	}

	c.namespaces.Store(ns.NamespaceName(), ns)

	return ns, nil
}

// DropNamespace deletes a namespace
func (c *Catalog) DropNamespace(ctx context.Context, namespace []string) error {
	name := strings.Join(namespace, ".")

	// Check for tables in namespace
	tables, err := c.store.ListTables(ctx, name)
	if err == nil && len(tables) > 0 {
		return errors.New("namespace is not empty")
	}

	if err := c.store.DeleteNamespace(ctx, name); err != nil {
		return err
	}

	c.namespaces.Delete(name)
	atomic.AddInt64(&c.metrics.NamespacesDeleted, 1)

	return nil
}

// CreateTable creates a new table
func (c *Catalog) CreateTable(ctx context.Context, namespace []string, name string, schema *Schema, spec *PartitionSpec, location string, properties map[string]string) (*Table, error) {
	nsName := strings.Join(namespace, ".")
	tableKey := nsName + "." + name

	// Check namespace exists
	if _, err := c.GetNamespace(ctx, namespace); err != nil {
		return nil, err
	}

	// Check table doesn't exist
	if _, ok := c.tables.Load(tableKey); ok {
		return nil, ErrTableAlreadyExists
	}

	// Generate table location
	if location == "" {
		location = fmt.Sprintf("%s%s/%s", c.config.WarehouseLocation, nsName, name)
	}

	// Create metadata
	tableUUID := uuid.New().String()
	now := time.Now().UnixMilli()

	if spec == nil {
		spec = &PartitionSpec{
			SpecID: 0,
			Fields: []*PartitionField{},
		}
	}

	metadata := &TableMetadata{
		FormatVersion:      2,
		TableUUID:          tableUUID,
		Location:           location,
		LastUpdatedMs:      now,
		LastColumnID:       getMaxFieldID(schema),
		Schema:             schema,
		Schemas:            []*Schema{schema},
		CurrentSchemaID:    schema.SchemaID,
		PartitionSpecs:     []*PartitionSpec{spec},
		DefaultSpecID:      spec.SpecID,
		LastPartitionID:    getMaxPartitionFieldID(spec),
		Properties:         properties,
		SortOrders:         []*SortOrder{{OrderID: 0, Fields: []*SortField{}}},
		DefaultSortOrderID: 0,
		Refs:               make(map[string]*SnapshotRef),
	}

	// Write metadata file
	metadataPath := fmt.Sprintf("%s/metadata/v1.metadata.json", location)
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	if err := c.warehouse.WriteMetadata(ctx, metadataPath, metadataBytes); err != nil {
		return nil, err
	}

	table := &Table{
		Identifier: TableIdentifier{
			Namespace: namespace,
			Name:      name,
		},
		Metadata:         metadata,
		MetadataLocation: metadataPath,
	}

	if err := c.store.PutTable(ctx, table); err != nil {
		return nil, err
	}

	c.tables.Store(tableKey, table)
	atomic.AddInt64(&c.metrics.TablesCreated, 1)

	return table, nil
}

// LoadTable loads a table
func (c *Catalog) LoadTable(ctx context.Context, namespace []string, name string) (*Table, error) {
	nsName := strings.Join(namespace, ".")
	tableKey := nsName + "." + name

	// Check cache
	if val, ok := c.tables.Load(tableKey); ok {
		atomic.AddInt64(&c.metrics.CacheHits, 1)
		return val.(*Table), nil
	}

	atomic.AddInt64(&c.metrics.CacheMisses, 1)

	table, err := c.store.GetTable(ctx, nsName, name)
	if err != nil {
		return nil, ErrTableNotFound
	}

	c.tables.Store(tableKey, table)
	return table, nil
}

// ListTables lists tables in a namespace
func (c *Catalog) ListTables(ctx context.Context, namespace []string) (*TableListResponse, error) {
	nsName := strings.Join(namespace, ".")

	tables, err := c.store.ListTables(ctx, nsName)
	if err != nil {
		return nil, err
	}

	result := &TableListResponse{
		Identifiers: make([]TableIdentifier, 0, len(tables)),
	}

	for _, table := range tables {
		result.Identifiers = append(result.Identifiers, table.Identifier)
	}

	return result, nil
}

// TableListResponse is the response for listing tables
type TableListResponse struct {
	Identifiers   []TableIdentifier `json:"identifiers"`
	NextPageToken string            `json:"next-page-token,omitempty"`
}

// DropTable deletes a table
func (c *Catalog) DropTable(ctx context.Context, namespace []string, name string, purge bool) error {
	nsName := strings.Join(namespace, ".")
	tableKey := nsName + "." + name

	table, err := c.LoadTable(ctx, namespace, name)
	if err != nil {
		return err
	}

	// If purge, delete data files
	if purge {
		if err := c.warehouse.DeleteMetadata(ctx, table.Metadata.Location); err != nil {
			// Log but continue
		}
	}

	if err := c.store.DeleteTable(ctx, nsName, name); err != nil {
		return err
	}

	c.tables.Delete(tableKey)
	atomic.AddInt64(&c.metrics.TablesDeleted, 1)

	return nil
}

// UpdateTableRequest contains table update operations
type UpdateTableRequest struct {
	Identifier   TableIdentifier   `json:"identifier"`
	Requirements []TableRequirement `json:"requirements"`
	Updates      []TableUpdate      `json:"updates"`
}

// TableRequirement defines a precondition for updates
type TableRequirement struct {
	Type              string  `json:"type"`
	Ref               string  `json:"ref,omitempty"`
	UUID              string  `json:"uuid,omitempty"`
	SnapshotID        *int64  `json:"snapshot-id,omitempty"`
	LastAssignedFieldID *int  `json:"last-assigned-field-id,omitempty"`
	LastAssignedPartitionID *int `json:"last-assigned-partition-id,omitempty"`
	DefaultSpecID     *int    `json:"default-spec-id,omitempty"`
	DefaultSortOrderID *int   `json:"default-sort-order-id,omitempty"`
	CurrentSchemaID   *int    `json:"current-schema-id,omitempty"`
}

// TableUpdate defines a table update operation
type TableUpdate struct {
	Action            string            `json:"action"`
	Schema            *Schema           `json:"schema,omitempty"`
	Spec              *PartitionSpec    `json:"spec,omitempty"`
	SortOrder         *SortOrder        `json:"sort-order,omitempty"`
	Location          string            `json:"location,omitempty"`
	Properties        map[string]string `json:"properties,omitempty"`
	Removals          []string          `json:"removals,omitempty"`
	Snapshot          *Snapshot         `json:"snapshot,omitempty"`
	RefName           string            `json:"ref-name,omitempty"`
	Ref               *SnapshotRef      `json:"ref,omitempty"`
	SchemaID          *int              `json:"schema-id,omitempty"`
	SpecID            *int              `json:"spec-id,omitempty"`
	OrderID           *int              `json:"order-id,omitempty"`
}

// CommitTable commits table updates
func (c *Catalog) CommitTable(ctx context.Context, request *UpdateTableRequest) (*Table, error) {
	table, err := c.LoadTable(ctx, request.Identifier.Namespace, request.Identifier.Name)
	if err != nil {
		return nil, err
	}

	// Validate requirements
	for _, req := range request.Requirements {
		if err := c.validateRequirement(table, &req); err != nil {
			atomic.AddInt64(&c.metrics.CommitConflicts, 1)
			atomic.AddInt64(&c.metrics.CommitsFailed, 1)
			return nil, err
		}
	}

	// Apply updates
	newMetadata := c.copyMetadata(table.Metadata)
	if newMetadata == nil {
		atomic.AddInt64(&c.metrics.CommitsFailed, 1)
		return nil, fmt.Errorf("failed to copy table metadata for update: internal serialization error")
	}
	for _, update := range request.Updates {
		if err := c.applyUpdate(newMetadata, &update); err != nil {
			atomic.AddInt64(&c.metrics.CommitsFailed, 1)
			return nil, err
		}
	}

	newMetadata.LastUpdatedMs = time.Now().UnixMilli()

	// Write new metadata file
	version := len(newMetadata.MetadataLog) + 2
	metadataPath := fmt.Sprintf("%s/metadata/v%d.metadata.json", newMetadata.Location, version)
	metadataBytes, err := json.Marshal(newMetadata)
	if err != nil {
		return nil, err
	}

	if err := c.warehouse.WriteMetadata(ctx, metadataPath, metadataBytes); err != nil {
		return nil, err
	}

	// Add to metadata log
	newMetadata.MetadataLog = append(newMetadata.MetadataLog, &MetadataLogEntry{
		TimestampMs:  newMetadata.LastUpdatedMs,
		MetadataFile: table.MetadataLocation,
	})

	table.Metadata = newMetadata
	table.MetadataLocation = metadataPath

	if err := c.store.PutTable(ctx, table); err != nil {
		return nil, err
	}

	tableKey := request.Identifier.FullName()
	c.tables.Store(tableKey, table)

	atomic.AddInt64(&c.metrics.CommitsSucceeded, 1)
	atomic.AddInt64(&c.metrics.TablesUpdated, 1)

	return table, nil
}

func (c *Catalog) validateRequirement(table *Table, req *TableRequirement) error {
	switch req.Type {
	case "assert-table-uuid":
		if req.UUID != table.Metadata.TableUUID {
			return ErrRequirementFailed
		}
	case "assert-ref-snapshot-id":
		ref, ok := table.Metadata.Refs[req.Ref]
		if !ok {
			if req.SnapshotID != nil {
				return ErrRequirementFailed
			}
		} else if req.SnapshotID != nil && ref.SnapshotID != *req.SnapshotID {
			return ErrRequirementFailed
		}
	case "assert-last-assigned-field-id":
		if req.LastAssignedFieldID != nil && *req.LastAssignedFieldID != table.Metadata.LastColumnID {
			return ErrRequirementFailed
		}
	case "assert-current-schema-id":
		if req.CurrentSchemaID != nil && *req.CurrentSchemaID != table.Metadata.CurrentSchemaID {
			return ErrRequirementFailed
		}
	case "assert-default-spec-id":
		if req.DefaultSpecID != nil && *req.DefaultSpecID != table.Metadata.DefaultSpecID {
			return ErrRequirementFailed
		}
	case "assert-default-sort-order-id":
		if req.DefaultSortOrderID != nil && *req.DefaultSortOrderID != table.Metadata.DefaultSortOrderID {
			return ErrRequirementFailed
		}
	}
	return nil
}

func (c *Catalog) applyUpdate(metadata *TableMetadata, update *TableUpdate) error {
	switch update.Action {
	case "upgrade-format-version":
		metadata.FormatVersion = 2
	case "add-schema":
		if update.Schema != nil {
			update.Schema.SchemaID = len(metadata.Schemas)
			metadata.Schemas = append(metadata.Schemas, update.Schema)
			metadata.LastColumnID = max(metadata.LastColumnID, getMaxFieldID(update.Schema))
		}
	case "set-current-schema":
		if update.SchemaID != nil {
			metadata.CurrentSchemaID = *update.SchemaID
			for _, s := range metadata.Schemas {
				if s.SchemaID == *update.SchemaID {
					metadata.Schema = s
					break
				}
			}
		}
	case "add-spec":
		if update.Spec != nil {
			update.Spec.SpecID = len(metadata.PartitionSpecs)
			metadata.PartitionSpecs = append(metadata.PartitionSpecs, update.Spec)
			metadata.LastPartitionID = max(metadata.LastPartitionID, getMaxPartitionFieldID(update.Spec))
		}
	case "set-default-spec":
		if update.SpecID != nil {
			metadata.DefaultSpecID = *update.SpecID
		}
	case "add-sort-order":
		if update.SortOrder != nil {
			update.SortOrder.OrderID = len(metadata.SortOrders)
			metadata.SortOrders = append(metadata.SortOrders, update.SortOrder)
		}
	case "set-default-sort-order":
		if update.OrderID != nil {
			metadata.DefaultSortOrderID = *update.OrderID
		}
	case "add-snapshot":
		if update.Snapshot != nil {
			metadata.Snapshots = append(metadata.Snapshots, update.Snapshot)
			metadata.SnapshotLog = append(metadata.SnapshotLog, &SnapshotLogEntry{
				TimestampMs: update.Snapshot.TimestampMs,
				SnapshotID:  update.Snapshot.SnapshotID,
			})
			atomic.AddInt64(&c.metrics.SnapshotsCreated, 1)
		}
	case "set-snapshot-ref":
		if metadata.Refs == nil {
			metadata.Refs = make(map[string]*SnapshotRef)
		}
		if update.Ref != nil {
			metadata.Refs[update.RefName] = update.Ref
			if update.RefName == "main" {
				metadata.CurrentSnapshotID = &update.Ref.SnapshotID
			}
		}
	case "remove-snapshot-ref":
		delete(metadata.Refs, update.RefName)
	case "set-location":
		metadata.Location = update.Location
	case "set-properties":
		if metadata.Properties == nil {
			metadata.Properties = make(map[string]string)
		}
		for k, v := range update.Properties {
			metadata.Properties[k] = v
		}
	case "remove-properties":
		for _, k := range update.Removals {
			delete(metadata.Properties, k)
		}
	}
	return nil
}

func (c *Catalog) copyMetadata(m *TableMetadata) *TableMetadata {
	if m == nil {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		log.Error().
			Err(err).
			Str("table_uuid", m.TableUUID).
			Msg("failed to marshal table metadata for copy - returning nil copy")
		return nil
	}
	var copy TableMetadata
	if err := json.Unmarshal(data, &copy); err != nil {
		log.Error().
			Err(err).
			Str("table_uuid", m.TableUUID).
			Msg("failed to unmarshal table metadata copy - returning nil copy")
		return nil
	}
	return &copy
}

// RenameTable renames a table
func (c *Catalog) RenameTable(ctx context.Context, fromNamespace []string, fromName string, toNamespace []string, toName string) error {
	table, err := c.LoadTable(ctx, fromNamespace, fromName)
	if err != nil {
		return err
	}

	// Update identifier
	table.Identifier = TableIdentifier{
		Namespace: toNamespace,
		Name:      toName,
	}

	// Store under new key
	fromKey := strings.Join(fromNamespace, ".") + "." + fromName
	toKey := strings.Join(toNamespace, ".") + "." + toName

	if err := c.store.PutTable(ctx, table); err != nil {
		return err
	}

	c.tables.Delete(fromKey)
	c.tables.Store(toKey, table)

	// Delete old entry
	return c.store.DeleteTable(ctx, strings.Join(fromNamespace, "."), fromName)
}

// CreateView creates a new view
func (c *Catalog) CreateView(ctx context.Context, namespace []string, name string, schema *Schema, sql string, dialect string) (*View, error) {
	nsName := strings.Join(namespace, ".")
	viewKey := nsName + "." + name

	// Check namespace exists
	if _, err := c.GetNamespace(ctx, namespace); err != nil {
		return nil, err
	}

	viewUUID := uuid.New().String()
	now := time.Now().UnixMilli()
	location := fmt.Sprintf("%s%s/%s", c.config.WarehouseLocation, nsName, name)

	version := &ViewVersion{
		VersionID:   1,
		SchemaID:    schema.SchemaID,
		TimestampMs: now,
		Summary:     map[string]string{"operation": "create"},
		Representations: []*ViewRepresentation{
			{
				Type:    "sql",
				SQL:     sql,
				Dialect: dialect,
			},
		},
		DefaultNamespace: namespace,
	}

	metadata := &ViewMetadata{
		FormatVersion:    1,
		ViewUUID:         viewUUID,
		Location:         location,
		CurrentVersionID: 1,
		Versions:         []*ViewVersion{version},
		VersionLog: []*ViewVersionLog{
			{TimestampMs: now, VersionID: 1},
		},
		Schemas: []*Schema{schema},
	}

	view := &View{
		Identifier: TableIdentifier{
			Namespace: namespace,
			Name:      name,
		},
		ViewMetadata:     metadata,
		MetadataLocation: fmt.Sprintf("%s/metadata/v1.metadata.json", location),
	}

	if err := c.store.PutView(ctx, view); err != nil {
		return nil, err
	}

	c.views.Store(viewKey, view)
	atomic.AddInt64(&c.metrics.ViewsCreated, 1)

	return view, nil
}

// LoadView loads a view
func (c *Catalog) LoadView(ctx context.Context, namespace []string, name string) (*View, error) {
	nsName := strings.Join(namespace, ".")
	viewKey := nsName + "." + name

	if val, ok := c.views.Load(viewKey); ok {
		return val.(*View), nil
	}

	view, err := c.store.GetView(ctx, nsName, name)
	if err != nil {
		return nil, ErrViewNotFound
	}

	c.views.Store(viewKey, view)
	return view, nil
}

// ListViews lists views in a namespace
func (c *Catalog) ListViews(ctx context.Context, namespace []string) (*TableListResponse, error) {
	nsName := strings.Join(namespace, ".")

	views, err := c.store.ListViews(ctx, nsName)
	if err != nil {
		return nil, err
	}

	result := &TableListResponse{
		Identifiers: make([]TableIdentifier, 0, len(views)),
	}

	for _, view := range views {
		result.Identifiers = append(result.Identifiers, view.Identifier)
	}

	return result, nil
}

// DropView deletes a view
func (c *Catalog) DropView(ctx context.Context, namespace []string, name string) error {
	nsName := strings.Join(namespace, ".")
	viewKey := nsName + "." + name

	if err := c.store.DeleteView(ctx, nsName, name); err != nil {
		return err
	}

	c.views.Delete(viewKey)
	return nil
}

// GetMetrics returns catalog metrics
func (c *Catalog) GetMetrics() *CatalogMetrics {
	c.metrics.mu.RLock()
	defer c.metrics.mu.RUnlock()

	return &CatalogMetrics{
		NamespacesCreated:  atomic.LoadInt64(&c.metrics.NamespacesCreated),
		NamespacesDeleted:  atomic.LoadInt64(&c.metrics.NamespacesDeleted),
		TablesCreated:      atomic.LoadInt64(&c.metrics.TablesCreated),
		TablesUpdated:      atomic.LoadInt64(&c.metrics.TablesUpdated),
		TablesDeleted:      atomic.LoadInt64(&c.metrics.TablesDeleted),
		SnapshotsCreated:   atomic.LoadInt64(&c.metrics.SnapshotsCreated),
		CommitsSucceeded:   atomic.LoadInt64(&c.metrics.CommitsSucceeded),
		CommitsFailed:      atomic.LoadInt64(&c.metrics.CommitsFailed),
		CommitConflicts:    atomic.LoadInt64(&c.metrics.CommitConflicts),
		ViewsCreated:       atomic.LoadInt64(&c.metrics.ViewsCreated),
		ViewsUpdated:       atomic.LoadInt64(&c.metrics.ViewsUpdated),
		CacheHits:          atomic.LoadInt64(&c.metrics.CacheHits),
		CacheMisses:        atomic.LoadInt64(&c.metrics.CacheMisses),
	}
}

// Helper functions

func getMaxFieldID(schema *Schema) int {
	if schema == nil {
		return 0
	}
	maxID := 0
	for _, field := range schema.Fields {
		if field.ID > maxID {
			maxID = field.ID
		}
	}
	return maxID
}

func getMaxPartitionFieldID(spec *PartitionSpec) int {
	if spec == nil {
		return 0
	}
	maxID := 0
	for _, field := range spec.Fields {
		if field.FieldID > maxID {
			maxID = field.FieldID
		}
	}
	return maxID
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// RESTHandler provides HTTP handlers for Iceberg REST API
type RESTHandler struct {
	catalog *Catalog
}

// NewRESTHandler creates a new REST handler
func NewRESTHandler(catalog *Catalog) *RESTHandler {
	return &RESTHandler{catalog: catalog}
}

// RegisterRoutes registers all Iceberg REST API routes
func (h *RESTHandler) RegisterRoutes(mux *http.ServeMux) {
	// Configuration
	mux.HandleFunc("/v1/config", h.handleConfig)

	// Namespaces
	mux.HandleFunc("/v1/namespaces", h.handleNamespaces)
	mux.HandleFunc("/v1/namespaces/", h.handleNamespace)

	// Tables
	mux.HandleFunc("/v1/namespaces/{namespace}/tables", h.handleTables)
	mux.HandleFunc("/v1/namespaces/{namespace}/tables/", h.handleTable)

	// Views
	mux.HandleFunc("/v1/namespaces/{namespace}/views", h.handleViews)
	mux.HandleFunc("/v1/namespaces/{namespace}/views/", h.handleView)
}

func (h *RESTHandler) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	config, err := h.catalog.GetConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

func (h *RESTHandler) handleNamespaces(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		parent := r.URL.Query().Get("parent")
		var parentNs []string
		if parent != "" {
			parentNs = strings.Split(parent, ".")
		}

		result, err := h.catalog.ListNamespaces(ctx, parentNs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)

	case http.MethodPost:
		var req struct {
			Namespace  []string          `json:"namespace"`
			Properties map[string]string `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		ns, err := h.catalog.CreateNamespace(ctx, req.Namespace, req.Properties)
		if err != nil {
			if errors.Is(err, ErrNamespaceAlreadyExists) {
				http.Error(w, err.Error(), http.StatusConflict)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(ns)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *RESTHandler) handleNamespace(w http.ResponseWriter, r *http.Request) {
	// Extract namespace from path
	path := strings.TrimPrefix(r.URL.Path, "/v1/namespaces/")
	namespace := strings.Split(path, "/")[0]
	nsparts := strings.Split(namespace, ".")

	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		ns, err := h.catalog.GetNamespace(ctx, nsparts)
		if err != nil {
			if errors.Is(err, ErrNamespaceNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ns)

	case http.MethodDelete:
		err := h.catalog.DropNamespace(ctx, nsparts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *RESTHandler) handleTables(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	nsparts := strings.Split(namespace, ".")
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		result, err := h.catalog.ListTables(ctx, nsparts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)

	case http.MethodPost:
		var req struct {
			Name       string            `json:"name"`
			Schema     *Schema           `json:"schema"`
			Spec       *PartitionSpec    `json:"partition-spec"`
			Location   string            `json:"location"`
			Properties map[string]string `json:"properties"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		table, err := h.catalog.CreateTable(ctx, nsparts, req.Name, req.Schema, req.Spec, req.Location, req.Properties)
		if err != nil {
			if errors.Is(err, ErrTableAlreadyExists) {
				http.Error(w, err.Error(), http.StatusConflict)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(table)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *RESTHandler) handleTable(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/namespaces/")
	parts := strings.Split(path, "/tables/")
	if len(parts) != 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	namespace := strings.Split(parts[0], ".")
	tableName := parts[1]
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		table, err := h.catalog.LoadTable(ctx, namespace, tableName)
		if err != nil {
			if errors.Is(err, ErrTableNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(table)

	case http.MethodPost:
		// Commit updates
		var req UpdateTableRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		req.Identifier = TableIdentifier{Namespace: namespace, Name: tableName}

		table, err := h.catalog.CommitTable(ctx, &req)
		if err != nil {
			if errors.Is(err, ErrCommitConflict) || errors.Is(err, ErrRequirementFailed) {
				http.Error(w, err.Error(), http.StatusConflict)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(table)

	case http.MethodDelete:
		purge := r.URL.Query().Get("purgeRequested") == "true"
		err := h.catalog.DropTable(ctx, namespace, tableName, purge)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *RESTHandler) handleViews(w http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	nsparts := strings.Split(namespace, ".")
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		result, err := h.catalog.ListViews(ctx, nsparts)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *RESTHandler) handleView(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/namespaces/")
	parts := strings.Split(path, "/views/")
	if len(parts) != 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	namespace := strings.Split(parts[0], ".")
	viewName := parts[1]
	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		view, err := h.catalog.LoadView(ctx, namespace, viewName)
		if err != nil {
			if errors.Is(err, ErrViewNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(view)

	case http.MethodDelete:
		err := h.catalog.DropView(ctx, namespace, viewName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// TableExists checks if a table exists
func (c *Catalog) TableExists(ctx context.Context, namespace []string, name string) bool {
	_, err := c.LoadTable(ctx, namespace, name)
	return err == nil
}

// GetTableSnapshot returns a specific snapshot
func (c *Catalog) GetTableSnapshot(ctx context.Context, namespace []string, name string, snapshotID int64) (*Snapshot, error) {
	table, err := c.LoadTable(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	for _, snap := range table.Metadata.Snapshots {
		if snap.SnapshotID == snapshotID {
			return snap, nil
		}
	}

	return nil, errors.New("snapshot not found")
}

// ListTableSnapshots lists all snapshots for a table
func (c *Catalog) ListTableSnapshots(ctx context.Context, namespace []string, name string) ([]*Snapshot, error) {
	table, err := c.LoadTable(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	// Sort by timestamp descending
	snapshots := make([]*Snapshot, len(table.Metadata.Snapshots))
	copy(snapshots, table.Metadata.Snapshots)
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].TimestampMs > snapshots[j].TimestampMs
	})

	return snapshots, nil
}

// RevertToSnapshot reverts a table to a previous snapshot
func (c *Catalog) RevertToSnapshot(ctx context.Context, namespace []string, name string, snapshotID int64) error {
	snapshot, err := c.GetTableSnapshot(ctx, namespace, name, snapshotID)
	if err != nil {
		return err
	}

	// Create update request to set the snapshot ref
	req := &UpdateTableRequest{
		Identifier: TableIdentifier{Namespace: namespace, Name: name},
		Updates: []TableUpdate{
			{
				Action:  "set-snapshot-ref",
				RefName: "main",
				Ref: &SnapshotRef{
					SnapshotID: snapshot.SnapshotID,
					Type:       "branch",
				},
			},
		},
	}

	_, err = c.CommitTable(ctx, req)
	return err
}

// Close closes the catalog and cleans up resources
func (c *Catalog) Close() error {
	// Clear caches
	c.namespaces = sync.Map{}
	c.tables = sync.Map{}
	c.views = sync.Map{}

	return nil
}

// Ensure RESTHandler implements io.Closer
var _ io.Closer = (*Catalog)(nil)
