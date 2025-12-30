package iceberg

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// MockMetadataStore implements MetadataStore for testing
// Fields ordered by size to minimize padding.
type MockMetadataStore struct {
	namespaces map[string]*Namespace
	tables     map[string]*Table
	views      map[string]*View
	mu         sync.RWMutex
}

func newMockMetadataStore() *MockMetadataStore {
	return &MockMetadataStore{
		namespaces: make(map[string]*Namespace),
		tables:     make(map[string]*Table),
		views:      make(map[string]*View),
	}
}

func (m *MockMetadataStore) GetNamespace(ctx context.Context, name string) (*Namespace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ns, ok := m.namespaces[name]
	if !ok {
		return nil, ErrNamespaceNotFound
	}
	return ns, nil
}

func (m *MockMetadataStore) PutNamespace(ctx context.Context, ns *Namespace) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.namespaces[ns.NamespaceName()] = ns
	return nil
}

func (m *MockMetadataStore) DeleteNamespace(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.namespaces, name)
	return nil
}

func (m *MockMetadataStore) ListNamespaces(ctx context.Context, parent string) ([]*Namespace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Namespace
	for _, ns := range m.namespaces {
		name := ns.NamespaceName()
		if parent == "" || (len(name) > len(parent) && name[:len(parent)] == parent) {
			result = append(result, ns)
		}
	}
	return result, nil
}

func (m *MockMetadataStore) GetTable(ctx context.Context, namespace, name string) (*Table, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := namespace + "." + name
	table, ok := m.tables[key]
	if !ok {
		return nil, ErrTableNotFound
	}
	return table, nil
}

func (m *MockMetadataStore) PutTable(ctx context.Context, table *Table) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := table.Identifier.FullName()
	m.tables[key] = table
	return nil
}

func (m *MockMetadataStore) DeleteTable(ctx context.Context, namespace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := namespace + "." + name
	delete(m.tables, key)
	return nil
}

func (m *MockMetadataStore) ListTables(ctx context.Context, namespace string) ([]*Table, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Table
	for key, table := range m.tables {
		if len(key) > len(namespace) && key[:len(namespace)] == namespace {
			result = append(result, table)
		}
	}
	return result, nil
}

func (m *MockMetadataStore) GetView(ctx context.Context, namespace, name string) (*View, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := namespace + "." + name
	view, ok := m.views[key]
	if !ok {
		return nil, ErrViewNotFound
	}
	return view, nil
}

func (m *MockMetadataStore) PutView(ctx context.Context, view *View) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := view.Identifier.FullName()
	m.views[key] = view
	return nil
}

func (m *MockMetadataStore) DeleteView(ctx context.Context, namespace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := namespace + "." + name
	delete(m.views, key)
	return nil
}

func (m *MockMetadataStore) ListViews(ctx context.Context, namespace string) ([]*View, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*View
	for key, view := range m.views {
		if len(key) > len(namespace) && key[:len(namespace)] == namespace {
			result = append(result, view)
		}
	}
	return result, nil
}

// MockWarehouseStore implements WarehouseStore for testing
// Fields ordered by size to minimize padding.
type MockWarehouseStore struct {
	files map[string][]byte
	mu    sync.RWMutex
}

func newMockWarehouseStore() *MockWarehouseStore {
	return &MockWarehouseStore{
		files: make(map[string][]byte),
	}
}

func (m *MockWarehouseStore) WriteMetadata(ctx context.Context, path string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.files[path] = data
	return nil
}

func (m *MockWarehouseStore) ReadMetadata(ctx context.Context, path string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, ok := m.files[path]
	if !ok {
		return nil, errors.New("file not found")
	}
	return data, nil
}

func (m *MockWarehouseStore) DeleteMetadata(ctx context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.files, path)
	return nil
}

func (m *MockWarehouseStore) ListFiles(ctx context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []string
	for path := range m.files {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			result = append(result, path)
		}
	}
	return result, nil
}

func TestCreateNamespace(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace
	ns, err := catalog.CreateNamespace(ctx, []string{"db1"}, map[string]string{"owner": "test"})
	if err != nil {
		t.Fatalf("CreateNamespace failed: %v", err)
	}

	if ns.NamespaceName() != "db1" {
		t.Errorf("Expected namespace 'db1', got '%s'", ns.NamespaceName())
	}

	if ns.Properties["owner"] != "test" {
		t.Errorf("Expected owner 'test', got '%s'", ns.Properties["owner"])
	}

	// Try to create duplicate
	_, err = catalog.CreateNamespace(ctx, []string{"db1"}, nil)
	if !errors.Is(err, ErrNamespaceAlreadyExists) {
		t.Errorf("Expected ErrNamespaceAlreadyExists, got %v", err)
	}

	// Check metrics
	metrics := catalog.GetMetrics()
	if metrics.NamespacesCreated != 1 {
		t.Errorf("Expected 1 namespace created, got %d", metrics.NamespacesCreated)
	}
}

func TestGetNamespace(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace first
	_, err := catalog.CreateNamespace(ctx, []string{"db1"}, nil)
	if err != nil {
		t.Fatalf("CreateNamespace failed: %v", err)
	}

	// Get namespace
	ns, err := catalog.GetNamespace(ctx, []string{"db1"})
	if err != nil {
		t.Fatalf("GetNamespace failed: %v", err)
	}

	if ns.NamespaceName() != "db1" {
		t.Errorf("Expected namespace 'db1', got '%s'", ns.NamespaceName())
	}

	// Get non-existent namespace
	_, err = catalog.GetNamespace(ctx, []string{"nonexistent"})
	if !errors.Is(err, ErrNamespaceNotFound) {
		t.Errorf("Expected ErrNamespaceNotFound, got %v", err)
	}
}

func TestListNamespaces(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespaces
	catalog.CreateNamespace(ctx, []string{"db1"}, nil)
	catalog.CreateNamespace(ctx, []string{"db2"}, nil)
	catalog.CreateNamespace(ctx, []string{"db3"}, nil)

	// List all namespaces
	result, err := catalog.ListNamespaces(ctx, nil)
	if err != nil {
		t.Fatalf("ListNamespaces failed: %v", err)
	}

	if len(result.Namespaces) != 3 {
		t.Errorf("Expected 3 namespaces, got %d", len(result.Namespaces))
	}
}

func TestDropNamespace(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create and drop namespace
	_, err := catalog.CreateNamespace(ctx, []string{"todelete"}, nil)
	if err != nil {
		t.Fatalf("CreateNamespace failed: %v", err)
	}

	err = catalog.DropNamespace(ctx, []string{"todelete"})
	if err != nil {
		t.Fatalf("DropNamespace failed: %v", err)
	}

	// Verify deleted
	_, err = catalog.GetNamespace(ctx, []string{"todelete"})
	if !errors.Is(err, ErrNamespaceNotFound) {
		t.Errorf("Expected ErrNamespaceNotFound, got %v", err)
	}

	// Check metrics
	metrics := catalog.GetMetrics()
	if metrics.NamespacesDeleted != 1 {
		t.Errorf("Expected 1 namespace deleted, got %d", metrics.NamespacesDeleted)
	}
}

func TestCreateTable(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace first
	_, err := catalog.CreateNamespace(ctx, []string{"testdb"}, nil)
	if err != nil {
		t.Fatalf("CreateNamespace failed: %v", err)
	}

	// Create schema
	schema := &Schema{
		SchemaID: 0,
		Fields: []*Field{
			{ID: 1, Name: "id", Required: true, Type: "long"},
			{ID: 2, Name: "name", Required: false, Type: "string"},
			{ID: 3, Name: "created_at", Required: false, Type: "timestamp"},
		},
	}

	// Create partition spec
	partitionSpec := &PartitionSpec{
		SpecID: 0,
		Fields: []*PartitionField{
			{SourceID: 3, FieldID: 1000, Name: "created_at_day", Transform: "day"},
		},
	}

	// Create table
	table, err := catalog.CreateTable(ctx, []string{"testdb"}, "users", schema, partitionSpec, "", nil)
	if err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}

	if table.Identifier.Name != "users" {
		t.Errorf("Expected table name 'users', got '%s'", table.Identifier.Name)
	}

	if table.Metadata.FormatVersion != 2 {
		t.Errorf("Expected format version 2, got %d", table.Metadata.FormatVersion)
	}

	if len(table.Metadata.Schema.Fields) != 3 {
		t.Errorf("Expected 3 schema fields, got %d", len(table.Metadata.Schema.Fields))
	}

	if len(table.Metadata.PartitionSpecs[0].Fields) != 1 {
		t.Errorf("Expected 1 partition field, got %d", len(table.Metadata.PartitionSpecs[0].Fields))
	}

	// Try to create duplicate
	_, err = catalog.CreateTable(ctx, []string{"testdb"}, "users", schema, nil, "", nil)
	if !errors.Is(err, ErrTableAlreadyExists) {
		t.Errorf("Expected ErrTableAlreadyExists, got %v", err)
	}

	// Check metrics
	metrics := catalog.GetMetrics()
	if metrics.TablesCreated != 1 {
		t.Errorf("Expected 1 table created, got %d", metrics.TablesCreated)
	}
}

func TestLoadTable(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and table
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}
	_, err := catalog.CreateTable(ctx, []string{"testdb"}, "mytable", schema, nil, "", nil)
	if err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}

	// Load table
	table, err := catalog.LoadTable(ctx, []string{"testdb"}, "mytable")
	if err != nil {
		t.Fatalf("LoadTable failed: %v", err)
	}

	if table.Identifier.Name != "mytable" {
		t.Errorf("Expected table name 'mytable', got '%s'", table.Identifier.Name)
	}

	// Load non-existent table
	_, err = catalog.LoadTable(ctx, []string{"testdb"}, "nonexistent")
	if !errors.Is(err, ErrTableNotFound) {
		t.Errorf("Expected ErrTableNotFound, got %v", err)
	}
}

func TestListTables(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and tables
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}

	catalog.CreateTable(ctx, []string{"testdb"}, "table1", schema, nil, "", nil)
	catalog.CreateTable(ctx, []string{"testdb"}, "table2", schema, nil, "", nil)
	catalog.CreateTable(ctx, []string{"testdb"}, "table3", schema, nil, "", nil)

	// List tables
	result, err := catalog.ListTables(ctx, []string{"testdb"})
	if err != nil {
		t.Fatalf("ListTables failed: %v", err)
	}

	if len(result.Identifiers) != 3 {
		t.Errorf("Expected 3 tables, got %d", len(result.Identifiers))
	}
}

func TestDropTable(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and table
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}
	catalog.CreateTable(ctx, []string{"testdb"}, "todelete", schema, nil, "", nil)

	// Drop table
	err := catalog.DropTable(ctx, []string{"testdb"}, "todelete", false)
	if err != nil {
		t.Fatalf("DropTable failed: %v", err)
	}

	// Verify deleted
	_, err = catalog.LoadTable(ctx, []string{"testdb"}, "todelete")
	if !errors.Is(err, ErrTableNotFound) {
		t.Errorf("Expected ErrTableNotFound, got %v", err)
	}

	// Check metrics
	metrics := catalog.GetMetrics()
	if metrics.TablesDeleted != 1 {
		t.Errorf("Expected 1 table deleted, got %d", metrics.TablesDeleted)
	}
}

func TestCommitTable(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and table
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}
	catalog.CreateTable(ctx, []string{"testdb"}, "committest", schema, nil, "", nil)

	// Commit updates
	request := &UpdateTableRequest{
		Identifier: TableIdentifier{Namespace: []string{"testdb"}, Name: "committest"},
		Updates: []TableUpdate{
			{
				Action: "set-properties",
				Properties: map[string]string{
					"description": "Test table",
					"owner":       "test-user",
				},
			},
		},
	}

	table, err := catalog.CommitTable(ctx, request)
	if err != nil {
		t.Fatalf("CommitTable failed: %v", err)
	}

	if table.Metadata.Properties["description"] != "Test table" {
		t.Errorf("Expected description 'Test table', got '%s'", table.Metadata.Properties["description"])
	}

	// Check metrics
	metrics := catalog.GetMetrics()
	if metrics.CommitsSucceeded != 1 {
		t.Errorf("Expected 1 commit succeeded, got %d", metrics.CommitsSucceeded)
	}

	if metrics.TablesUpdated != 1 {
		t.Errorf("Expected 1 table updated, got %d", metrics.TablesUpdated)
	}
}

func TestAddSnapshot(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and table
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}
	catalog.CreateTable(ctx, []string{"testdb"}, "snaptest", schema, nil, "", nil)

	// Add snapshot
	snapshot := &Snapshot{
		SnapshotID:     12345,
		SequenceNumber: 1,
		TimestampMs:    time.Now().UnixMilli(),
		ManifestList:   "s3://warehouse/testdb/snaptest/metadata/snap-12345.avro",
		Summary: map[string]string{
			"operation":     "append",
			"added-records": "100",
		},
	}

	request := &UpdateTableRequest{
		Identifier: TableIdentifier{Namespace: []string{"testdb"}, Name: "snaptest"},
		Updates: []TableUpdate{
			{Action: "add-snapshot", Snapshot: snapshot},
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

	table, err := catalog.CommitTable(ctx, request)
	if err != nil {
		t.Fatalf("CommitTable failed: %v", err)
	}

	if len(table.Metadata.Snapshots) != 1 {
		t.Errorf("Expected 1 snapshot, got %d", len(table.Metadata.Snapshots))
	}

	if table.Metadata.CurrentSnapshotID == nil || *table.Metadata.CurrentSnapshotID != 12345 {
		t.Errorf("Expected current snapshot ID 12345")
	}

	// Check metrics
	metrics := catalog.GetMetrics()
	if metrics.SnapshotsCreated != 1 {
		t.Errorf("Expected 1 snapshot created, got %d", metrics.SnapshotsCreated)
	}
}

func TestRequirementValidation(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and table
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}
	table, _ := catalog.CreateTable(ctx, []string{"testdb"}, "reqtest", schema, nil, "", nil)

	// Commit with valid UUID requirement
	request := &UpdateTableRequest{
		Identifier: TableIdentifier{Namespace: []string{"testdb"}, Name: "reqtest"},
		Requirements: []TableRequirement{
			{Type: "assert-table-uuid", UUID: table.Metadata.TableUUID},
		},
		Updates: []TableUpdate{
			{Action: "set-properties", Properties: map[string]string{"test": "value"}},
		},
	}

	_, err := catalog.CommitTable(ctx, request)
	if err != nil {
		t.Fatalf("CommitTable with valid requirement failed: %v", err)
	}

	// Commit with invalid UUID requirement
	request.Requirements[0].UUID = "wrong-uuid"
	_, err = catalog.CommitTable(ctx, request)
	if !errors.Is(err, ErrRequirementFailed) {
		t.Errorf("Expected ErrRequirementFailed, got %v", err)
	}

	// Check metrics
	metrics := catalog.GetMetrics()
	if metrics.CommitConflicts != 1 {
		t.Errorf("Expected 1 commit conflict, got %d", metrics.CommitConflicts)
	}
}

func TestCreateView(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	// Create view
	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "count", Type: "long"}},
	}

	view, err := catalog.CreateView(ctx, []string{"testdb"}, "user_count", schema, "SELECT COUNT(*) as count FROM users", "spark")
	if err != nil {
		t.Fatalf("CreateView failed: %v", err)
	}

	if view.Identifier.Name != "user_count" {
		t.Errorf("Expected view name 'user_count', got '%s'", view.Identifier.Name)
	}

	if view.ViewMetadata.CurrentVersionID != 1 {
		t.Errorf("Expected version 1, got %d", view.ViewMetadata.CurrentVersionID)
	}

	// Check metrics
	metrics := catalog.GetMetrics()
	if metrics.ViewsCreated != 1 {
		t.Errorf("Expected 1 view created, got %d", metrics.ViewsCreated)
	}
}

func TestLoadView(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and view
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "count", Type: "long"}},
	}
	catalog.CreateView(ctx, []string{"testdb"}, "myview", schema, "SELECT 1", "spark")

	// Load view
	view, err := catalog.LoadView(ctx, []string{"testdb"}, "myview")
	if err != nil {
		t.Fatalf("LoadView failed: %v", err)
	}

	if view.Identifier.Name != "myview" {
		t.Errorf("Expected view name 'myview', got '%s'", view.Identifier.Name)
	}

	// Load non-existent view
	_, err = catalog.LoadView(ctx, []string{"testdb"}, "nonexistent")
	if !errors.Is(err, ErrViewNotFound) {
		t.Errorf("Expected ErrViewNotFound, got %v", err)
	}
}

func TestRenameTable(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespaces and table
	catalog.CreateNamespace(ctx, []string{"db1"}, nil)
	catalog.CreateNamespace(ctx, []string{"db2"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}
	catalog.CreateTable(ctx, []string{"db1"}, "oldname", schema, nil, "", nil)

	// Rename table
	err := catalog.RenameTable(ctx, []string{"db1"}, "oldname", []string{"db2"}, "newname")
	if err != nil {
		t.Fatalf("RenameTable failed: %v", err)
	}

	// Verify old name doesn't exist
	_, err = catalog.LoadTable(ctx, []string{"db1"}, "oldname")
	if !errors.Is(err, ErrTableNotFound) {
		t.Errorf("Expected ErrTableNotFound for old name, got %v", err)
	}

	// Verify new name exists
	table, err := catalog.LoadTable(ctx, []string{"db2"}, "newname")
	if err != nil {
		t.Fatalf("LoadTable with new name failed: %v", err)
	}

	if table.Identifier.Name != "newname" {
		t.Errorf("Expected table name 'newname', got '%s'", table.Identifier.Name)
	}
}

func TestTableExists(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and table
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}
	catalog.CreateTable(ctx, []string{"testdb"}, "exists", schema, nil, "", nil)

	// Check existing table
	if !catalog.TableExists(ctx, []string{"testdb"}, "exists") {
		t.Error("Expected table to exist")
	}

	// Check non-existing table
	if catalog.TableExists(ctx, []string{"testdb"}, "notexists") {
		t.Error("Expected table to not exist")
	}
}

func TestListTableSnapshots(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and table
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}
	catalog.CreateTable(ctx, []string{"testdb"}, "snapshots", schema, nil, "", nil)

	// Add multiple snapshots
	for i := int64(1); i <= 3; i++ {
		snapshot := &Snapshot{
			SnapshotID:     i * 1000,
			SequenceNumber: i,
			TimestampMs:    time.Now().UnixMilli() + i*1000,
			ManifestList:   "s3://warehouse/manifest.avro",
			Summary:        map[string]string{"operation": "append"},
		}

		request := &UpdateTableRequest{
			Identifier: TableIdentifier{Namespace: []string{"testdb"}, Name: "snapshots"},
			Updates: []TableUpdate{
				{Action: "add-snapshot", Snapshot: snapshot},
			},
		}
		catalog.CommitTable(ctx, request)
	}

	// List snapshots
	snapshots, err := catalog.ListTableSnapshots(ctx, []string{"testdb"}, "snapshots")
	if err != nil {
		t.Fatalf("ListTableSnapshots failed: %v", err)
	}

	if len(snapshots) != 3 {
		t.Errorf("Expected 3 snapshots, got %d", len(snapshots))
	}

	// Should be sorted by timestamp descending
	if snapshots[0].SnapshotID != 3000 {
		t.Errorf("Expected first snapshot ID 3000, got %d", snapshots[0].SnapshotID)
	}
}

func TestCacheMetrics(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	// First access - cache hit (CreateNamespace already cached it)
	catalog.GetNamespace(ctx, []string{"testdb"})

	// Second access - cache hit
	catalog.GetNamespace(ctx, []string{"testdb"})

	metrics := catalog.GetMetrics()
	// Both calls are cache hits since CreateNamespace already stored the namespace in cache
	if metrics.CacheHits != 2 {
		t.Errorf("Expected 2 cache hits, got %d", metrics.CacheHits)
	}

	// Note: Cache misses count includes initial creation
}

func TestGetConfig(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	config := &CatalogConfig{
		CatalogName:       "test-catalog",
		WarehouseLocation: "s3://test-warehouse/",
	}
	catalog := NewCatalog(store, warehouse, config)

	ctx := context.Background()

	resp, err := catalog.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if resp.Defaults["warehouse"] != "s3://test-warehouse/" {
		t.Errorf("Expected warehouse 's3://test-warehouse/', got '%s'", resp.Defaults["warehouse"])
	}
}

func TestSchemaEvolution(t *testing.T) {
	store := newMockMetadataStore()
	warehouse := newMockWarehouseStore()
	catalog := NewCatalog(store, warehouse, nil)

	ctx := context.Background()

	// Create namespace and table
	catalog.CreateNamespace(ctx, []string{"testdb"}, nil)

	schema := &Schema{
		SchemaID: 0,
		Fields:   []*Field{{ID: 1, Name: "id", Type: "long"}},
	}
	catalog.CreateTable(ctx, []string{"testdb"}, "evolve", schema, nil, "", nil)

	// Add new schema
	newSchema := &Schema{
		SchemaID: 1,
		Fields: []*Field{
			{ID: 1, Name: "id", Type: "long"},
			{ID: 2, Name: "name", Type: "string"},
		},
	}

	request := &UpdateTableRequest{
		Identifier: TableIdentifier{Namespace: []string{"testdb"}, Name: "evolve"},
		Updates: []TableUpdate{
			{Action: "add-schema", Schema: newSchema},
			{Action: "set-current-schema", SchemaID: intPtr(1)},
		},
	}

	table, err := catalog.CommitTable(ctx, request)
	if err != nil {
		t.Fatalf("CommitTable failed: %v", err)
	}

	if len(table.Metadata.Schemas) != 2 {
		t.Errorf("Expected 2 schemas, got %d", len(table.Metadata.Schemas))
	}

	if table.Metadata.CurrentSchemaID != 1 {
		t.Errorf("Expected current schema ID 1, got %d", table.Metadata.CurrentSchemaID)
	}

	if len(table.Metadata.Schema.Fields) != 2 {
		t.Errorf("Expected 2 fields in current schema, got %d", len(table.Metadata.Schema.Fields))
	}
}

func intPtr(i int) *int {
	return &i
}

// TestCopyMetadataErrorHandling verifies that copyMetadata handles errors properly
// and callers check for nil returns
func TestCopyMetadataErrorHandling(t *testing.T) {
	catalog := &Catalog{
		config:  &CatalogConfig{CatalogName: "test"},
		metrics: &CatalogMetrics{},
	}

	t.Run("valid metadata returns copy", func(t *testing.T) {
		validMetadata := &TableMetadata{
			FormatVersion: 2,
			TableUUID:     "test-uuid",
			Location:      "s3://bucket/table",
		}

		copied, err := catalog.copyMetadata(validMetadata)
		if err != nil {
			t.Fatalf("copyMetadata should not return error for valid metadata: %v", err)
		}
		if copied == nil {
			t.Fatal("copyMetadata should return non-nil for valid metadata")
		}

		if copied.TableUUID != validMetadata.TableUUID {
			t.Errorf("Expected TableUUID %s, got %s", validMetadata.TableUUID, copied.TableUUID)
		}

		// Verify that original is not modified when copy is changed
		copied.Location = "modified"
		if validMetadata.Location == "modified" {
			t.Error("Modifying copy should not affect original metadata")
		}
	})

	t.Run("nil input returns error", func(t *testing.T) {
		// Test with nil input - should return error
		// Note: This tests defensive programming; copyMetadata should not panic on nil
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("copyMetadata panicked on nil input: %v", r)
			}
		}()

		// Nil metadata should return error
		copied, err := catalog.copyMetadata(nil)
		if err == nil {
			t.Error("copyMetadata with nil input should return error")
		}
		if copied != nil {
			t.Error("copyMetadata with nil input should return nil result")
		}
	})

	t.Run("complex metadata with nested structures", func(t *testing.T) {
		// Test with more complex metadata structure
		complexMetadata := &TableMetadata{
			FormatVersion:   2,
			TableUUID:       "complex-uuid",
			Location:        "s3://bucket/complex-table",
			LastUpdatedMs:   1234567890,
			LastColumnID:    10,
			CurrentSchemaID: 1,
			Schema: &Schema{
				SchemaID: 1,
				Fields: []*Field{
					{ID: 1, Name: "id", Type: "long", Required: true},
					{ID: 2, Name: "data", Type: "string", Required: false},
				},
			},
			Schemas: []*Schema{
				{SchemaID: 0, Fields: []*Field{{ID: 1, Name: "id", Type: "long"}}},
				{SchemaID: 1, Fields: []*Field{{ID: 1, Name: "id", Type: "long"}, {ID: 2, Name: "data", Type: "string"}}},
			},
			Properties: map[string]string{
				"owner":       "test-user",
				"created-at":  "2024-01-01",
				"description": "Test table with nested structures",
			},
		}

		copied, err := catalog.copyMetadata(complexMetadata)
		if err != nil {
			t.Fatalf("copyMetadata should not return error for complex metadata: %v", err)
		}
		if copied == nil {
			t.Fatal("copyMetadata should return non-nil for complex metadata")
		}

		// Verify deep copy of nested structures
		if len(copied.Schema.Fields) != len(complexMetadata.Schema.Fields) {
			t.Errorf("Expected %d fields, got %d", len(complexMetadata.Schema.Fields), len(copied.Schema.Fields))
		}

		if len(copied.Schemas) != len(complexMetadata.Schemas) {
			t.Errorf("Expected %d schemas, got %d", len(complexMetadata.Schemas), len(copied.Schemas))
		}

		if len(copied.Properties) != len(complexMetadata.Properties) {
			t.Errorf("Expected %d properties, got %d", len(complexMetadata.Properties), len(copied.Properties))
		}

		// Verify modifying copy doesn't affect original
		copied.Properties["new-key"] = "new-value"
		if _, exists := complexMetadata.Properties["new-key"]; exists {
			t.Error("Modifying copied properties should not affect original")
		}
	})
}
