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
)

// Iceberg Catalog Errors.
var (
	ErrNamespaceNotFound      = errors.New("namespace not found")
	ErrNamespaceAlreadyExists = errors.New("namespace already exists")
	ErrTableNotFound          = errors.New("table not found")
	ErrTableAlreadyExists     = errors.New("table already exists")
	ErrInvalidSchema          = errors.New("invalid schema")
	ErrCommitConflict         = errors.New("commit conflict: table was modified")
	ErrViewNotFound           = errors.New("view not found")
	ErrRequirementFailed      = errors.New("requirement failed")
)

// Catalog implements the Iceberg REST Catalog API.
type Catalog struct {
	store      MetadataStore
	warehouse  WarehouseStore
	config     *CatalogConfig
	metrics    *CatalogMetrics
	namespaces sync.Map
	tables     sync.Map
	views      sync.Map
}

// CatalogConfig configures the Iceberg catalog.
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

// DefaultCatalogConfig returns sensible defaults.
func DefaultCatalogConfig() *CatalogConfig {
	return &CatalogConfig{
		CatalogName:       "nebulaio",
		WarehouseLocation: "s3://warehouse/",
		DefaultNamespace:  "default",
		EnableCaching:     true,
		CacheTTL:          5 * time.Minute,
	}
}

// MetadataStore interface for catalog metadata persistence.
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

// WarehouseStore interface for data file storage.
type WarehouseStore interface {
	WriteMetadata(ctx context.Context, path string, data []byte) error
	ReadMetadata(ctx context.Context, path string) ([]byte, error)
	DeleteMetadata(ctx context.Context, path string) error
	ListFiles(ctx context.Context, prefix string) ([]string, error)
}

// Namespace represents an Iceberg namespace (database).
type Namespace struct {
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
	Properties map[string]string `json:"properties,omitempty"`
	Name       []string          `json:"namespace"`
}

// NamespaceName returns the full namespace name.
func (n *Namespace) NamespaceName() string {
	return strings.Join(n.Name, ".")
}

// Table represents an Iceberg table.
type Table struct {
	Metadata         *TableMetadata    `json:"metadata"`
	Config           map[string]string `json:"config,omitempty"`
	Identifier       TableIdentifier   `json:"identifier"`
	MetadataLocation string            `json:"metadata-location"`
}

// TableIdentifier identifies a table.
type TableIdentifier struct {
	Name      string   `json:"name"`
	Namespace []string `json:"namespace"`
}

// FullName returns the full table name.
func (t *TableIdentifier) FullName() string {
	return strings.Join(t.Namespace, ".") + "." + t.Name
}

// TableMetadata contains Iceberg table metadata.
type TableMetadata struct {
	Properties          map[string]string       `json:"properties,omitempty"`
	Refs                map[string]*SnapshotRef `json:"refs,omitempty"`
	CurrentSnapshotID   *int64                  `json:"current-snapshot-id,omitempty"`
	Schema              *Schema                 `json:"schema"`
	TableUUID           string                  `json:"table-uuid"`
	Location            string                  `json:"location"`
	MetadataLog         []*MetadataLogEntry     `json:"metadata-log,omitempty"`
	SortOrders          []*SortOrder            `json:"sort-orders,omitempty"`
	PartitionSpecs      []*PartitionSpec        `json:"partition-specs"`
	PartitionStatistics []*PartitionStatistics  `json:"partition-statistics,omitempty"`
	StatisticsFiles     []*StatisticsFile       `json:"statistics,omitempty"`
	Schemas             []*Schema               `json:"schemas,omitempty"`
	SnapshotLog         []*SnapshotLogEntry     `json:"snapshot-log,omitempty"`
	Snapshots           []*Snapshot             `json:"snapshots,omitempty"`
	LastColumnID        int                     `json:"last-column-id"`
	FormatVersion       int                     `json:"format-version"`
	CurrentSchemaID     int                     `json:"current-schema-id"`
	DefaultSortOrderID  int                     `json:"default-sort-order-id"`
	LastUpdatedMs       int64                   `json:"last-updated-ms"`
	LastPartitionID     int                     `json:"last-partition-id"`
	DefaultSpecID       int                     `json:"default-spec-id"`
}

// Schema represents an Iceberg schema.
type Schema struct {
	IdentifierFieldIDs []int    `json:"identifier-field-ids,omitempty"`
	Fields             []*Field `json:"fields"`
	SchemaID           int      `json:"schema-id"`
}

// Field represents a schema field.
type Field struct {
	Type           interface{} `json:"type"`
	InitialDefault interface{} `json:"initial-default,omitempty"`
	WriteDefault   interface{} `json:"write-default,omitempty"`
	Name           string      `json:"name"`
	Doc            string      `json:"doc,omitempty"`
	ID             int         `json:"id"`
	Required       bool        `json:"required"`
}

// PartitionSpec defines table partitioning.
type PartitionSpec struct {
	Fields []*PartitionField `json:"fields"`
	SpecID int               `json:"spec-id"`
}

// PartitionField defines a partition field.
type PartitionField struct {
	Name      string `json:"name"`
	Transform string `json:"transform"`
	SourceID  int    `json:"source-id"`
	FieldID   int    `json:"field-id"`
}

// Snapshot represents a table snapshot.
type Snapshot struct {
	ParentSnapshotID *int64            `json:"parent-snapshot-id,omitempty"`
	Summary          map[string]string `json:"summary"`
	SchemaID         *int              `json:"schema-id,omitempty"`
	ManifestList     string            `json:"manifest-list"`
	SnapshotID       int64             `json:"snapshot-id"`
	SequenceNumber   int64             `json:"sequence-number"`
	TimestampMs      int64             `json:"timestamp-ms"`
}

// SnapshotLogEntry records snapshot history.
type SnapshotLogEntry struct {
	TimestampMs int64 `json:"timestamp-ms"`
	SnapshotID  int64 `json:"snapshot-id"`
}

// MetadataLogEntry records metadata file history.
type MetadataLogEntry struct {
	MetadataFile string `json:"metadata-file"`
	TimestampMs  int64  `json:"timestamp-ms"`
}

// SortOrder defines table sort order.
type SortOrder struct {
	Fields  []*SortField `json:"fields"`
	OrderID int          `json:"order-id"`
}

// SortField defines a sort field.
type SortField struct {
	Transform string `json:"transform"`
	Direction string `json:"direction"`
	NullOrder string `json:"null-order"`
	SourceID  int    `json:"source-id"`
}

// SnapshotRef is a named reference to a snapshot.
type SnapshotRef struct {
	MaxRefAgeMs        *int64 `json:"max-ref-age-ms,omitempty"`
	MaxSnapshotAgeMs   *int64 `json:"max-snapshot-age-ms,omitempty"`
	MinSnapshotsToKeep *int   `json:"min-snapshots-to-keep,omitempty"`
	Type               string `json:"type"`
	SnapshotID         int64  `json:"snapshot-id"`
}

// StatisticsFile contains table statistics.
type StatisticsFile struct {
	StatisticsPath        string         `json:"statistics-path"`
	BlobMetadata          []BlobMetadata `json:"blob-metadata"`
	SnapshotID            int64          `json:"snapshot-id"`
	FileSizeInBytes       int64          `json:"file-size-in-bytes"`
	FileFooterSizeInBytes int64          `json:"file-footer-size-in-bytes"`
}

// BlobMetadata for statistics.
type BlobMetadata struct {
	Properties     map[string]string `json:"properties,omitempty"`
	Type           string            `json:"type"`
	Fields         []int             `json:"fields"`
	SnapshotID     int64             `json:"snapshot-id"`
	SequenceNumber int64             `json:"sequence-number"`
}

// PartitionStatistics contains partition-level statistics.
type PartitionStatistics struct {
	StatisticsPath  string `json:"statistics-path"`
	SnapshotID      int64  `json:"snapshot-id"`
	FileSizeInBytes int64  `json:"file-size-in-bytes"`
}

// View represents an Iceberg view.
type View struct {
	Identifier       TableIdentifier `json:"identifier"`
	ViewMetadata     *ViewMetadata   `json:"metadata"`
	MetadataLocation string          `json:"metadata-location"`
}

// ViewMetadata contains Iceberg view metadata.
type ViewMetadata struct {
	Properties       map[string]string `json:"properties,omitempty"`
	ViewUUID         string            `json:"view-uuid"`
	Location         string            `json:"location"`
	Versions         []*ViewVersion    `json:"versions"`
	VersionLog       []*ViewVersionLog `json:"version-log"`
	Schemas          []*Schema         `json:"schemas"`
	FormatVersion    int               `json:"format-version"`
	CurrentVersionID int               `json:"current-version-id"`
}

// ViewVersion represents a view version.
type ViewVersion struct {
	Summary          map[string]string     `json:"summary"`
	DefaultCatalog   string                `json:"default-catalog,omitempty"`
	Representations  []*ViewRepresentation `json:"representations"`
	DefaultNamespace []string              `json:"default-namespace,omitempty"`
	VersionID        int                   `json:"version-id"`
	SchemaID         int                   `json:"schema-id"`
	TimestampMs      int64                 `json:"timestamp-ms"`
}

// ViewRepresentation is a SQL representation of a view.
type ViewRepresentation struct {
	Type    string `json:"type"` // "sql"
	SQL     string `json:"sql"`
	Dialect string `json:"dialect"`
}

// ViewVersionLog records view version history.
type ViewVersionLog struct {
	TimestampMs int64 `json:"timestamp-ms"`
	VersionID   int   `json:"version-id"`
}

// CatalogMetrics tracks catalog performance.
type CatalogMetrics struct {
	mu                sync.RWMutex
	NamespacesCreated int64
	NamespacesDeleted int64
	TablesCreated     int64
	TablesUpdated     int64
	TablesDeleted     int64
	SnapshotsCreated  int64
	CommitsSucceeded  int64
	CommitsFailed     int64
	CommitConflicts   int64
	ViewsCreated      int64
	ViewsUpdated      int64
	CacheHits         int64
	CacheMisses       int64
}

// NewCatalog creates a new Iceberg catalog.
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

// GetConfig returns the catalog configuration.
func (c *Catalog) GetConfig(ctx context.Context) (*CatalogConfigResponse, error) {
	return &CatalogConfigResponse{
		Defaults: map[string]string{
			"warehouse": c.config.WarehouseLocation,
		},
		Overrides: map[string]string{},
	}, nil
}

// CatalogConfigResponse is the response for config endpoint.
type CatalogConfigResponse struct {
	Defaults  map[string]string `json:"defaults"`
	Overrides map[string]string `json:"overrides"`
}

// CreateNamespace creates a new namespace.
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

	err := c.store.PutNamespace(ctx, ns)
	if err != nil {
		return nil, err
	}

	c.namespaces.Store(name, ns)
	atomic.AddInt64(&c.metrics.NamespacesCreated, 1)

	return ns, nil
}

// GetNamespace retrieves a namespace.
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

// ListNamespaces lists all namespaces.
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

// NamespaceListResponse is the response for listing namespaces.
type NamespaceListResponse struct {
	NextPageToken string     `json:"next-page-token,omitempty"`
	Namespaces    [][]string `json:"namespaces"`
}

// UpdateNamespace updates namespace properties.
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

	err = c.store.PutNamespace(ctx, ns)
	if err != nil {
		return nil, err
	}

	c.namespaces.Store(ns.NamespaceName(), ns)

	return ns, nil
}

// DropNamespace deletes a namespace.
func (c *Catalog) DropNamespace(ctx context.Context, namespace []string) error {
	name := strings.Join(namespace, ".")

	// Check for tables in namespace
	tables, err := c.store.ListTables(ctx, name)
	if err == nil && len(tables) > 0 {
		return errors.New("namespace is not empty")
	}

	err = c.store.DeleteNamespace(ctx, name)
	if err != nil {
		return err
	}

	c.namespaces.Delete(name)
	atomic.AddInt64(&c.metrics.NamespacesDeleted, 1)

	return nil
}

// CreateTable creates a new table.
func (c *Catalog) CreateTable(ctx context.Context, namespace []string, name string, schema *Schema, spec *PartitionSpec, location string, properties map[string]string) (*Table, error) {
	nsName := strings.Join(namespace, ".")
	tableKey := nsName + "." + name

	// Check namespace exists
	_, err := c.GetNamespace(ctx, namespace)
	if err != nil {
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
	metadataPath := location + "/metadata/v1.metadata.json"

	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	err = c.warehouse.WriteMetadata(ctx, metadataPath, metadataBytes)
	if err != nil {
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

	err = c.store.PutTable(ctx, table)
	if err != nil {
		return nil, err
	}

	c.tables.Store(tableKey, table)
	atomic.AddInt64(&c.metrics.TablesCreated, 1)

	return table, nil
}

// LoadTable loads a table.
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

// ListTables lists tables in a namespace.
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

// TableListResponse is the response for listing tables.
type TableListResponse struct {
	NextPageToken string            `json:"next-page-token,omitempty"`
	Identifiers   []TableIdentifier `json:"identifiers"`
}

// DropTable deletes a table.
func (c *Catalog) DropTable(ctx context.Context, namespace []string, name string, purge bool) error {
	nsName := strings.Join(namespace, ".")
	tableKey := nsName + "." + name

	table, err := c.LoadTable(ctx, namespace, name)
	if err != nil {
		return err
	}

	// If purge, delete data files
	if purge {
		err = c.warehouse.DeleteMetadata(ctx, table.Metadata.Location)
		if err != nil {
			// Log but continue
		}
	}

	err = c.store.DeleteTable(ctx, nsName, name)
	if err != nil {
		return err
	}

	c.tables.Delete(tableKey)
	atomic.AddInt64(&c.metrics.TablesDeleted, 1)

	return nil
}

// UpdateTableRequest contains table update operations.
type UpdateTableRequest struct {
	Identifier   TableIdentifier    `json:"identifier"`
	Requirements []TableRequirement `json:"requirements"`
	Updates      []TableUpdate      `json:"updates"`
}

// TableRequirement defines a precondition for updates.
type TableRequirement struct {
	SnapshotID              *int64 `json:"snapshot-id,omitempty"`
	LastAssignedFieldID     *int   `json:"last-assigned-field-id,omitempty"`
	LastAssignedPartitionID *int   `json:"last-assigned-partition-id,omitempty"`
	DefaultSpecID           *int   `json:"default-spec-id,omitempty"`
	DefaultSortOrderID      *int   `json:"default-sort-order-id,omitempty"`
	CurrentSchemaID         *int   `json:"current-schema-id,omitempty"`
	Type                    string `json:"type"`
	Ref                     string `json:"ref,omitempty"`
	UUID                    string `json:"uuid,omitempty"`
}

// TableUpdate defines a table update operation.
type TableUpdate struct {
	Ref        *SnapshotRef      `json:"ref,omitempty"`
	Schema     *Schema           `json:"schema,omitempty"`
	Spec       *PartitionSpec    `json:"spec,omitempty"`
	SortOrder  *SortOrder        `json:"sort-order,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	Snapshot   *Snapshot         `json:"snapshot,omitempty"`
	SchemaID   *int              `json:"schema-id,omitempty"`
	SpecID     *int              `json:"spec-id,omitempty"`
	OrderID    *int              `json:"order-id,omitempty"`
	Location   string            `json:"location,omitempty"`
	RefName    string            `json:"ref-name,omitempty"`
	Action     string            `json:"action"`
	Removals   []string          `json:"removals,omitempty"`
}

// CommitTable commits table updates.
func (c *Catalog) CommitTable(ctx context.Context, request *UpdateTableRequest) (*Table, error) {
	table, err := c.LoadTable(ctx, request.Identifier.Namespace, request.Identifier.Name)
	if err != nil {
		return nil, err
	}

	// Validate requirements
	for _, req := range request.Requirements {
		err = c.validateRequirement(table, &req)
		if err != nil {
			atomic.AddInt64(&c.metrics.CommitConflicts, 1)
			atomic.AddInt64(&c.metrics.CommitsFailed, 1)

			return nil, err
		}
	}

	// Apply updates
	newMetadata, err := c.copyMetadata(table.Metadata)
	if err != nil {
		atomic.AddInt64(&c.metrics.CommitsFailed, 1)
		return nil, fmt.Errorf("failed to copy table metadata for update: %w", err)
	}

	for _, update := range request.Updates {
		err = c.applyUpdate(newMetadata, &update)
		if err != nil {
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

	err = c.warehouse.WriteMetadata(ctx, metadataPath, metadataBytes)
	if err != nil {
		return nil, err
	}

	// Add to metadata log
	newMetadata.MetadataLog = append(newMetadata.MetadataLog, &MetadataLogEntry{
		TimestampMs:  newMetadata.LastUpdatedMs,
		MetadataFile: table.MetadataLocation,
	})

	table.Metadata = newMetadata
	table.MetadataLocation = metadataPath

	err = c.store.PutTable(ctx, table)
	if err != nil {
		return nil, err
	}

	tableKey := request.Identifier.FullName()
	c.tables.Store(tableKey, table)

	atomic.AddInt64(&c.metrics.CommitsSucceeded, 1)
	atomic.AddInt64(&c.metrics.TablesUpdated, 1)

	return table, nil
}

func (c *Catalog) validateRequirement(table *Table, req *TableRequirement) error {
	validators := map[string]func(*Table, *TableRequirement) error{
		"assert-table-uuid":               c.validateTableUUID,
		"assert-ref-snapshot-id":          c.validateRefSnapshotID,
		"assert-last-assigned-field-id":   c.validateLastAssignedFieldID,
		"assert-current-schema-id":        c.validateCurrentSchemaID,
		"assert-default-spec-id":          c.validateDefaultSpecID,
		"assert-default-sort-order-id":    c.validateDefaultSortOrderID,
	}

	if validator, exists := validators[req.Type]; exists {
		return validator(table, req)
	}

	return nil
}

func (c *Catalog) validateTableUUID(table *Table, req *TableRequirement) error {
	if req.UUID != table.Metadata.TableUUID {
		return ErrRequirementFailed
	}
	return nil
}

func (c *Catalog) validateRefSnapshotID(table *Table, req *TableRequirement) error {
	ref, ok := table.Metadata.Refs[req.Ref]
	if !ok {
		if req.SnapshotID != nil {
			return ErrRequirementFailed
		}
		return nil
	}

	if req.SnapshotID != nil && ref.SnapshotID != *req.SnapshotID {
		return ErrRequirementFailed
	}

	return nil
}

func (c *Catalog) validateLastAssignedFieldID(table *Table, req *TableRequirement) error {
	if req.LastAssignedFieldID != nil && *req.LastAssignedFieldID != table.Metadata.LastColumnID {
		return ErrRequirementFailed
	}
	return nil
}

func (c *Catalog) validateCurrentSchemaID(table *Table, req *TableRequirement) error {
	if req.CurrentSchemaID != nil && *req.CurrentSchemaID != table.Metadata.CurrentSchemaID {
		return ErrRequirementFailed
	}
	return nil
}

func (c *Catalog) validateDefaultSpecID(table *Table, req *TableRequirement) error {
	if req.DefaultSpecID != nil && *req.DefaultSpecID != table.Metadata.DefaultSpecID {
		return ErrRequirementFailed
	}
	return nil
}

func (c *Catalog) validateDefaultSortOrderID(table *Table, req *TableRequirement) error {
	if req.DefaultSortOrderID != nil && *req.DefaultSortOrderID != table.Metadata.DefaultSortOrderID {
		return ErrRequirementFailed
	}
	return nil
}

func (c *Catalog) applyUpdate(metadata *TableMetadata, update *TableUpdate) error {
	switch update.Action {
	case "upgrade-format-version":
		c.applyUpgradeFormatVersion(metadata)
	case "add-schema":
		c.applyAddSchema(metadata, update)
	case "set-current-schema":
		c.applySetCurrentSchema(metadata, update)
	case "add-spec":
		c.applyAddSpec(metadata, update)
	case "set-default-spec":
		c.applySetDefaultSpec(metadata, update)
	case "add-sort-order":
		c.applyAddSortOrder(metadata, update)
	case "set-default-sort-order":
		c.applySetDefaultSortOrder(metadata, update)
	case "add-snapshot":
		c.applyAddSnapshot(metadata, update)
	case "set-snapshot-ref":
		c.applySetSnapshotRef(metadata, update)
	case "remove-snapshot-ref":
		c.applyRemoveSnapshotRef(metadata, update)
	case "set-location":
		c.applySetLocation(metadata, update)
	case "set-properties":
		c.applySetProperties(metadata, update)
	case "remove-properties":
		c.applyRemoveProperties(metadata, update)
	}

	return nil
}

// applyUpgradeFormatVersion upgrades the table format version.
func (c *Catalog) applyUpgradeFormatVersion(metadata *TableMetadata) {
	metadata.FormatVersion = 2
}

// applyAddSchema adds a new schema to the table.
func (c *Catalog) applyAddSchema(metadata *TableMetadata, update *TableUpdate) {
	if update.Schema != nil {
		update.Schema.SchemaID = len(metadata.Schemas)
		metadata.Schemas = append(metadata.Schemas, update.Schema)
		metadata.LastColumnID = max(metadata.LastColumnID, getMaxFieldID(update.Schema))
	}
}

// applySetCurrentSchema sets the current schema for the table.
func (c *Catalog) applySetCurrentSchema(metadata *TableMetadata, update *TableUpdate) {
	if update.SchemaID != nil {
		metadata.CurrentSchemaID = *update.SchemaID
		for _, s := range metadata.Schemas {
			if s.SchemaID == *update.SchemaID {
				metadata.Schema = s
				break
			}
		}
	}
}

// applyAddSpec adds a partition spec to the table.
func (c *Catalog) applyAddSpec(metadata *TableMetadata, update *TableUpdate) {
	if update.Spec != nil {
		update.Spec.SpecID = len(metadata.PartitionSpecs)
		metadata.PartitionSpecs = append(metadata.PartitionSpecs, update.Spec)
		metadata.LastPartitionID = max(metadata.LastPartitionID, getMaxPartitionFieldID(update.Spec))
	}
}

// applySetDefaultSpec sets the default partition spec.
func (c *Catalog) applySetDefaultSpec(metadata *TableMetadata, update *TableUpdate) {
	if update.SpecID != nil {
		metadata.DefaultSpecID = *update.SpecID
	}
}

// applyAddSortOrder adds a sort order to the table.
func (c *Catalog) applyAddSortOrder(metadata *TableMetadata, update *TableUpdate) {
	if update.SortOrder != nil {
		update.SortOrder.OrderID = len(metadata.SortOrders)
		metadata.SortOrders = append(metadata.SortOrders, update.SortOrder)
	}
}

// applySetDefaultSortOrder sets the default sort order.
func (c *Catalog) applySetDefaultSortOrder(metadata *TableMetadata, update *TableUpdate) {
	if update.OrderID != nil {
		metadata.DefaultSortOrderID = *update.OrderID
	}
}

// applyAddSnapshot adds a new snapshot to the table.
func (c *Catalog) applyAddSnapshot(metadata *TableMetadata, update *TableUpdate) {
	if update.Snapshot != nil {
		metadata.Snapshots = append(metadata.Snapshots, update.Snapshot)
		metadata.SnapshotLog = append(metadata.SnapshotLog, &SnapshotLogEntry{
			TimestampMs: update.Snapshot.TimestampMs,
			SnapshotID:  update.Snapshot.SnapshotID,
		})

		atomic.AddInt64(&c.metrics.SnapshotsCreated, 1)
	}
}

// applySetSnapshotRef sets a snapshot reference.
func (c *Catalog) applySetSnapshotRef(metadata *TableMetadata, update *TableUpdate) {
	if metadata.Refs == nil {
		metadata.Refs = make(map[string]*SnapshotRef)
	}

	if update.Ref != nil {
		metadata.Refs[update.RefName] = update.Ref
		if update.RefName == "main" {
			metadata.CurrentSnapshotID = &update.Ref.SnapshotID
		}
	}
}

// applyRemoveSnapshotRef removes a snapshot reference.
func (c *Catalog) applyRemoveSnapshotRef(metadata *TableMetadata, update *TableUpdate) {
	delete(metadata.Refs, update.RefName)
}

// applySetLocation sets the table location.
func (c *Catalog) applySetLocation(metadata *TableMetadata, update *TableUpdate) {
	metadata.Location = update.Location
}

// applySetProperties sets table properties.
func (c *Catalog) applySetProperties(metadata *TableMetadata, update *TableUpdate) {
	if metadata.Properties == nil {
		metadata.Properties = make(map[string]string)
	}

	for k, v := range update.Properties {
		metadata.Properties[k] = v
	}
}

// applyRemoveProperties removes table properties.
func (c *Catalog) applyRemoveProperties(metadata *TableMetadata, update *TableUpdate) {
	for _, k := range update.Removals {
		delete(metadata.Properties, k)
	}
}

func (c *Catalog) copyMetadata(m *TableMetadata) (*TableMetadata, error) {
	if m == nil {
		return nil, errors.New("cannot copy nil metadata")
	}

	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal table metadata for copy: %w", err)
	}

	var tableCopy TableMetadata
	err = json.Unmarshal(data, &tableCopy)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal table metadata copy: %w", err)
	}

	return &tableCopy, nil
}

// RenameTable renames a table.
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

	err = c.store.PutTable(ctx, table)
	if err != nil {
		return err
	}

	c.tables.Delete(fromKey)
	c.tables.Store(toKey, table)

	// Delete old entry
	return c.store.DeleteTable(ctx, strings.Join(fromNamespace, "."), fromName)
}

// CreateView creates a new view.
func (c *Catalog) CreateView(ctx context.Context, namespace []string, name string, schema *Schema, sql string, dialect string) (*View, error) {
	nsName := strings.Join(namespace, ".")
	viewKey := nsName + "." + name

	// Check namespace exists
	_, err := c.GetNamespace(ctx, namespace)
	if err != nil {
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
		MetadataLocation: location + "/metadata/v1.metadata.json",
	}

	err = c.store.PutView(ctx, view)
	if err != nil {
		return nil, err
	}

	c.views.Store(viewKey, view)
	atomic.AddInt64(&c.metrics.ViewsCreated, 1)

	return view, nil
}

// LoadView loads a view.
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

// ListViews lists views in a namespace.
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

// DropView deletes a view.
func (c *Catalog) DropView(ctx context.Context, namespace []string, name string) error {
	nsName := strings.Join(namespace, ".")
	viewKey := nsName + "." + name

	err := c.store.DeleteView(ctx, nsName, name)
	if err != nil {
		return err
	}

	c.views.Delete(viewKey)

	return nil
}

// GetMetrics returns catalog metrics.
func (c *Catalog) GetMetrics() *CatalogMetrics {
	c.metrics.mu.RLock()
	defer c.metrics.mu.RUnlock()

	return &CatalogMetrics{
		NamespacesCreated: atomic.LoadInt64(&c.metrics.NamespacesCreated),
		NamespacesDeleted: atomic.LoadInt64(&c.metrics.NamespacesDeleted),
		TablesCreated:     atomic.LoadInt64(&c.metrics.TablesCreated),
		TablesUpdated:     atomic.LoadInt64(&c.metrics.TablesUpdated),
		TablesDeleted:     atomic.LoadInt64(&c.metrics.TablesDeleted),
		SnapshotsCreated:  atomic.LoadInt64(&c.metrics.SnapshotsCreated),
		CommitsSucceeded:  atomic.LoadInt64(&c.metrics.CommitsSucceeded),
		CommitsFailed:     atomic.LoadInt64(&c.metrics.CommitsFailed),
		CommitConflicts:   atomic.LoadInt64(&c.metrics.CommitConflicts),
		ViewsCreated:      atomic.LoadInt64(&c.metrics.ViewsCreated),
		ViewsUpdated:      atomic.LoadInt64(&c.metrics.ViewsUpdated),
		CacheHits:         atomic.LoadInt64(&c.metrics.CacheHits),
		CacheMisses:       atomic.LoadInt64(&c.metrics.CacheMisses),
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

// RESTHandler provides HTTP handlers for Iceberg REST API.
type RESTHandler struct {
	catalog *Catalog
}

// NewRESTHandler creates a new REST handler.
func NewRESTHandler(catalog *Catalog) *RESTHandler {
	return &RESTHandler{catalog: catalog}
}

// RegisterRoutes registers all Iceberg REST API routes.
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
			Properties map[string]string `json:"properties"`
			Namespace  []string          `json:"namespace"`
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
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
			Schema     *Schema           `json:"schema"`
			Spec       *PartitionSpec    `json:"partition-spec"`
			Properties map[string]string `json:"properties"`
			Name       string            `json:"name"`
			Location   string            `json:"location"`
		}
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
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
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
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

// TableExists checks if a table exists.
func (c *Catalog) TableExists(ctx context.Context, namespace []string, name string) bool {
	_, err := c.LoadTable(ctx, namespace, name)
	return err == nil
}

// GetTableSnapshot returns a specific snapshot.
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

// ListTableSnapshots lists all snapshots for a table.
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

// RevertToSnapshot reverts a table to a previous snapshot.
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

// Close closes the catalog and cleans up resources.
func (c *Catalog) Close() error {
	// Clear caches
	c.namespaces = sync.Map{}
	c.tables = sync.Map{}
	c.views = sync.Map{}

	return nil
}

// Ensure RESTHandler implements io.Closer.
var _ io.Closer = (*Catalog)(nil)
