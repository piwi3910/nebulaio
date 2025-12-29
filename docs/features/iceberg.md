# Apache Iceberg Integration

NebulaIO provides native Apache Iceberg table format support, enabling data lakehouse workloads with ACID transactions, time travel, and schema evolution.

## Overview

Apache Iceberg is an open table format designed for huge analytic datasets. NebulaIO's integration provides:

- **REST Catalog API** - Standard Iceberg REST catalog interface
- **ACID Transactions** - Full transactional guarantees
- **Time Travel** - Query historical snapshots
- **Schema Evolution** - Safely evolve table schemas
- **Partition Evolution** - Change partitioning without rewriting data

## Features

### Native Table Support

Create and manage Iceberg tables directly through NebulaIO:

```sql

-- Create table via Spark with NebulaIO catalog
CREATE TABLE nebulaio.db.events (
    id BIGINT,
    event_type STRING,
    timestamp TIMESTAMP,
    data STRING
)
USING iceberg
PARTITIONED BY (days(timestamp));

```bash

### REST Catalog

Standard Iceberg REST Catalog API on port 9006:

```bash

# List namespaces
curl http://localhost:9006/iceberg/v1/namespaces

# Get table metadata
curl http://localhost:9006/iceberg/v1/namespaces/db/tables/events

```bash

### ACID Transactions

Full transaction support with snapshot isolation:

```python

from pyiceberg.catalog import load_catalog

catalog = load_catalog("nebulaio")
table = catalog.load_table("db.events")

# Atomic write
with table.new_append() as writer:
    writer.append(dataframe)

```bash

## Configuration

### Basic Setup

```yaml

iceberg:
  enabled: true
  catalog_type: rest         # rest | hive | glue
  warehouse: s3://warehouse/
  catalog_name: nebulaio
  enable_acid: true
  snapshot_retention: 5      # Keep last 5 snapshots

```bash

### Advanced Configuration

```yaml

iceberg:
  enabled: true
  catalog_type: rest
  warehouse: s3://warehouse/
  catalog_name: nebulaio
  enable_acid: true
  snapshot_retention: 10
  metadata_refresh_interval: 60  # Refresh metadata every 60s
  rest_catalog:
    port: 9006
    prefix: /iceberg
  # Hive metastore (optional)
  hive:
    uri: thrift://hive-metastore:9083
  # AWS Glue (optional)
  glue:
    catalog_id: "123456789012"
    region: us-east-1

```bash

### Environment Variables

| Variable | Description | Default |
| ---------- | ------------- | --------- |
| `NEBULAIO_ICEBERG_ENABLED` | Enable Iceberg support | `false` |
| `NEBULAIO_ICEBERG_CATALOG_TYPE` | Catalog type | `rest` |
| `NEBULAIO_ICEBERG_WAREHOUSE` | Warehouse location | `s3://warehouse/` |

## Usage Examples

### Spark Integration

```python

from pyspark.sql import SparkSession

spark = SparkSession.builder \
    .appName("NebulaIO Iceberg") \
    .config("spark.sql.catalog.nebulaio", "org.apache.iceberg.spark.SparkCatalog") \
    .config("spark.sql.catalog.nebulaio.type", "rest") \
    .config("spark.sql.catalog.nebulaio.uri", "http://localhost:9006/iceberg") \
    .config("spark.sql.catalog.nebulaio.warehouse", "s3://warehouse/") \
    .config("spark.sql.catalog.nebulaio.s3.endpoint", "http://localhost:9000") \
    .getOrCreate()

# Create namespace
spark.sql("CREATE NAMESPACE nebulaio.analytics")

# Create table
spark.sql("""
    CREATE TABLE nebulaio.analytics.events (
        id BIGINT,
        event_type STRING,
        timestamp TIMESTAMP
    )
    USING iceberg
    PARTITIONED BY (days(timestamp))
""")

# Insert data
df.writeTo("nebulaio.analytics.events").append()

```bash

### PyIceberg

```python

from pyiceberg.catalog import load_catalog
from pyiceberg.schema import Schema
from pyiceberg.types import NestedField, LongType, StringType

# Connect to catalog
catalog = load_catalog(
    "nebulaio",
    **{
        "type": "rest",
        "uri": "http://localhost:9006/iceberg",
        "warehouse": "s3://warehouse/",
        "s3.endpoint": "http://localhost:9000",
    }
)

# Create namespace
catalog.create_namespace("analytics")

# Create table
schema = Schema(
    NestedField(1, "id", LongType(), required=True),
    NestedField(2, "name", StringType()),
)
catalog.create_table("analytics.users", schema=schema)

# Load and query
table = catalog.load_table("analytics.users")
scan = table.scan()
df = scan.to_pandas()

```bash

### Trino Integration

```sql

-- Configure Trino connector
-- In etc/catalog/nebulaio.properties:
-- connector.name=iceberg
-- iceberg.catalog.type=rest
-- iceberg.rest-catalog.uri=http://nebulaio:9006/iceberg

-- Query Iceberg tables
SELECT * FROM nebulaio.analytics.events
WHERE timestamp > TIMESTAMP '2024-01-01 00:00:00';

-- Time travel
SELECT * FROM nebulaio.analytics.events
FOR VERSION AS OF 123456789;

```bash

## Time Travel

Query historical snapshots:

```python

# Get table snapshots
table = catalog.load_table("db.events")
for snapshot in table.snapshots():
    print(f"Snapshot {snapshot.snapshot_id}: {snapshot.timestamp_ms}")

# Query specific snapshot
scan = table.scan(snapshot_id=123456789)
df = scan.to_pandas()

```bash

## Schema Evolution

Safely evolve schemas:

```python

# Add column
table.update_schema() \
    .add_column("new_field", StringType()) \
    .commit()

# Rename column
table.update_schema() \
    .rename_column("old_name", "new_name") \
    .commit()

# Change type (if compatible)
table.update_schema() \
    .update_column("field", IntegerType(), LongType()) \
    .commit()

```bash

## Partition Evolution

Change partitioning without rewriting data:

```python

# Add new partition field
table.update_spec() \
    .add_field("category") \
    .commit()

# New writes use updated spec
# Old data remains unchanged

```bash

## Performance Tuning

### Optimal Settings

```yaml

iceberg:
  enabled: true
  snapshot_retention: 10
  metadata_refresh_interval: 30

# Combine with S3 Express for fast metadata
s3_express:
  enabled: true

```bash

### Table Properties

```sql

ALTER TABLE nebulaio.db.events
SET TBLPROPERTIES (
    'write.target-file-size-bytes' = '134217728',  -- 128MB
    'write.parquet.compression-codec' = 'zstd',
    'read.split.target-size' = '134217728'
);

```bash

## REST Catalog API

### Endpoints

| Endpoint | Method | Description |
| ---------- | -------- | ------------- |
| `/v1/namespaces` | GET | List namespaces |
| `/v1/namespaces` | POST | Create namespace |
| `/v1/namespaces/{ns}` | GET | Get namespace |
| `/v1/namespaces/{ns}/tables` | GET | List tables |
| `/v1/namespaces/{ns}/tables` | POST | Create table |
| `/v1/namespaces/{ns}/tables/{table}` | GET | Get table metadata |

### Example Requests

```bash

# Create namespace
curl -X POST http://localhost:9006/iceberg/v1/namespaces \
  -H "Content-Type: application/json" \
  -d '{"namespace": ["analytics"], "properties": {}}'

# Create table
curl -X POST http://localhost:9006/iceberg/v1/namespaces/analytics/tables \
  -H "Content-Type: application/json" \
  -d '{
    "name": "events",
    "schema": {
      "type": "struct",
      "fields": [
        {"id": 1, "name": "id", "type": "long", "required": true},
        {"id": 2, "name": "data", "type": "string", "required": false}
      ]
    }
  }'

```bash

## Troubleshooting

### Common Issues

1. **Metadata not refreshing**: Increase `metadata_refresh_interval`
2. **Transaction conflicts**: Use optimistic concurrency retry
3. **Snapshot cleanup**: Adjust `snapshot_retention`

### Logs

Enable debug logging:

```yaml

log_level: debug

```

Look for logs with `iceberg` tag.

## See Also

- [S3 Express One Zone](s3-express.md) - Fast metadata storage
- [NVIDIA NIM](nim.md) - AI inference on Iceberg data
- [GPUDirect Storage](gpudirect.md) - Fast data loading
