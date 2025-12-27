# S3 Select

NebulaIO's S3 Select enables running SQL queries directly on CSV, JSON, and Parquet files without downloading the entire object. This can reduce data transfer by up to 400x for selective queries.

## Overview

S3 Select pushes query processing to the storage layer, returning only the data matching your SQL query. This dramatically reduces bandwidth, latency, and client-side processing for analytical queries.

## Supported Formats

| Format | Compression | Description |
|--------|-------------|-------------|
| CSV | None, GZIP, BZIP2 | Comma-separated values with configurable delimiters |
| JSON | None, GZIP, BZIP2 | JSON Lines (one JSON object per line) or JSON Document |
| Parquet | Snappy, GZIP | Columnar format with automatic schema detection |

## Configuration

```yaml
s3select:
  enabled: true
  max_record_size: 1048576      # Maximum record size (1MB)
  max_results_size: 268435456   # Maximum results size (256MB)
  timeout: 300                  # Query timeout in seconds
  worker_pool_size: 10          # Concurrent query workers
```

## SQL Syntax

### Basic SELECT

```sql
SELECT * FROM s3object
SELECT column1, column2 FROM s3object
SELECT column1 AS alias FROM s3object
```

### WHERE Clause

```sql
SELECT * FROM s3object s WHERE s.age > 25
SELECT * FROM s3object s WHERE s.name = 'John'
SELECT * FROM s3object s WHERE s.status IN ('active', 'pending')
SELECT * FROM s3object s WHERE s.created BETWEEN '2024-01-01' AND '2024-12-31'
```

### Operators

| Operator | Description | Example |
|----------|-------------|---------|
| =, <>, <, >, <=, >= | Comparison | `age > 25` |
| AND, OR, NOT | Logical | `age > 25 AND status = 'active'` |
| IN | Set membership | `status IN ('a', 'b')` |
| BETWEEN | Range | `age BETWEEN 18 AND 65` |
| LIKE | Pattern match | `name LIKE 'John%'` |
| IS NULL, IS NOT NULL | Null check | `email IS NOT NULL` |

### Functions

**String Functions:**
```sql
SELECT UPPER(name), LOWER(email), TRIM(description) FROM s3object
SELECT SUBSTRING(name, 1, 5) FROM s3object
SELECT CHAR_LENGTH(name) FROM s3object
```

**Numeric Functions:**
```sql
SELECT ABS(value), FLOOR(price), CEIL(score) FROM s3object
SELECT ROUND(price, 2) FROM s3object
```

**Date Functions:**
```sql
SELECT EXTRACT(YEAR FROM created_at) FROM s3object
SELECT DATE_ADD(day, 7, created_at) FROM s3object
SELECT DATE_DIFF(day, start_date, end_date) FROM s3object
SELECT UTCNOW() FROM s3object
```

**Aggregate Functions:**
```sql
SELECT COUNT(*), SUM(amount), AVG(score) FROM s3object
SELECT MIN(price), MAX(price) FROM s3object
```

**Conditional Functions:**
```sql
SELECT CASE WHEN age > 18 THEN 'adult' ELSE 'minor' END FROM s3object
SELECT COALESCE(nickname, name, 'unknown') FROM s3object
SELECT NULLIF(status, 'pending') FROM s3object
```

### JSON-specific Syntax

For JSON data, use bracket notation:

```sql
-- Access nested fields
SELECT s.user.name, s.user.email FROM s3object s

-- Access array elements
SELECT s.items[0].name FROM s3object s

-- Wildcard for arrays
SELECT s.items[*].price FROM s3object s
```

## API Usage

### AWS SDK (Go)

```go
import (
    "github.com/aws/aws-sdk-go-v2/service/s3"
    "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

input := &s3.SelectObjectContentInput{
    Bucket: aws.String("my-bucket"),
    Key:    aws.String("data/sales.csv"),
    Expression: aws.String("SELECT * FROM s3object s WHERE s.amount > 1000"),
    ExpressionType: types.ExpressionTypeSql,
    InputSerialization: &types.InputSerialization{
        CSV: &types.CSVInput{
            FileHeaderInfo: types.FileHeaderInfoUse,
            FieldDelimiter: aws.String(","),
        },
        CompressionType: types.CompressionTypeGzip,
    },
    OutputSerialization: &types.OutputSerialization{
        JSON: &types.JSONOutput{
            RecordDelimiter: aws.String("\n"),
        },
    },
}

result, err := client.SelectObjectContent(ctx, input)
```

### AWS SDK (Python)

```python
import boto3

s3 = boto3.client('s3',
    endpoint_url='http://localhost:9000',
    aws_access_key_id='ACCESS_KEY',
    aws_secret_access_key='SECRET_KEY'
)

response = s3.select_object_content(
    Bucket='my-bucket',
    Key='data/sales.json',
    Expression="SELECT s.customer, s.amount FROM s3object s WHERE s.amount > 1000",
    ExpressionType='SQL',
    InputSerialization={
        'JSON': {'Type': 'LINES'},
        'CompressionType': 'GZIP'
    },
    OutputSerialization={
        'JSON': {'RecordDelimiter': '\n'}
    }
)

for event in response['Payload']:
    if 'Records' in event:
        print(event['Records']['Payload'].decode('utf-8'))
```

### cURL

```bash
# Note: S3 Select requires AWS Signature V4
aws s3api select-object-content \
  --endpoint-url http://localhost:9000 \
  --bucket my-bucket \
  --key data/logs.csv \
  --expression "SELECT * FROM s3object s WHERE s.level = 'ERROR'" \
  --expression-type SQL \
  --input-serialization '{"CSV": {"FileHeaderInfo": "USE"}}' \
  --output-serialization '{"JSON": {}}' \
  output.json
```

## Input Serialization Options

### CSV

```json
{
  "CSV": {
    "FileHeaderInfo": "USE",      // USE | IGNORE | NONE
    "FieldDelimiter": ",",        // Field separator
    "RecordDelimiter": "\n",      // Record separator
    "QuoteCharacter": "\"",       // Quote character
    "QuoteEscapeCharacter": "\\", // Escape character
    "Comments": "#",              // Comment prefix
    "AllowQuotedRecordDelimiter": true
  },
  "CompressionType": "GZIP"       // NONE | GZIP | BZIP2
}
```

### JSON

```json
{
  "JSON": {
    "Type": "LINES"               // LINES | DOCUMENT
  },
  "CompressionType": "GZIP"
}
```

### Parquet

```json
{
  "Parquet": {}
}
// Parquet is self-describing, no additional options needed
```

## Output Serialization Options

### CSV Output

```json
{
  "CSV": {
    "FieldDelimiter": ",",
    "RecordDelimiter": "\n",
    "QuoteCharacter": "\"",
    "QuoteEscapeCharacter": "\\",
    "QuoteFields": "ALWAYS"       // ALWAYS | ASNEEDED
  }
}
```

### JSON Output

```json
{
  "JSON": {
    "RecordDelimiter": "\n"
  }
}
```

## Performance

### Query Optimization

| Optimization | Description | Impact |
|--------------|-------------|--------|
| Column Pruning | Only requested columns are read | Up to 10x faster |
| Predicate Pushdown | Filters applied during scan | Up to 100x less data |
| Parquet Row Groups | Skip irrelevant row groups | Up to 50x faster |

### Benchmarks

| Scenario | Full Download | S3 Select | Speedup |
|----------|--------------|-----------|---------|
| 10GB CSV, 1% match | 45s | 0.8s | 56x |
| 1GB JSON, filter by field | 8s | 0.2s | 40x |
| 100GB Parquet, 3 columns | 180s | 12s | 15x |

## Use Cases

### Log Analysis

```sql
-- Find error logs in the last hour
SELECT timestamp, message, stack_trace
FROM s3object s
WHERE s.level = 'ERROR'
  AND s.timestamp > '2024-01-15T10:00:00Z'
```

### Data Filtering

```sql
-- Extract high-value transactions
SELECT customer_id, amount, transaction_date
FROM s3object s
WHERE s.amount > 10000
```

### Analytics Queries

```sql
-- Calculate regional totals
SELECT region, SUM(CAST(amount AS DECIMAL)) as total
FROM s3object s
GROUP BY region
```

### Schema Discovery

```sql
-- Sample first 10 records
SELECT * FROM s3object s LIMIT 10
```

## Limitations

| Limitation | Value |
|------------|-------|
| Maximum SQL expression length | 256 KB |
| Maximum record/row size | 1 MB |
| Maximum uncompressed row group (Parquet) | 256 MB |
| Maximum results returned | Configurable (default 256 MB) |
| Nested JSON depth | 10 levels |

## Error Handling

### Common Errors

| Error Code | Description | Solution |
|------------|-------------|----------|
| InvalidQuery | SQL syntax error | Check query syntax |
| MalformedCSV | CSV parsing failed | Verify delimiter settings |
| InvalidJSON | JSON parsing failed | Ensure valid JSON Lines format |
| TruncatedInput | Compressed data incomplete | Re-upload object |
| OverMaxRecordSize | Record exceeds limit | Split large records |

### Error Response Format

```json
{
  "Error": {
    "Code": "InvalidQuery",
    "Message": "Syntax error at line 1, column 15"
  }
}
```

## Best Practices

1. **Use column projection**: Only select needed columns to reduce I/O
2. **Filter early**: Put most selective predicates first in WHERE clause
3. **Use Parquet**: Columnar format enables best query performance
4. **Compress data**: GZIP or Snappy reduces storage and transfer
5. **Partition data**: Store data in prefix-based partitions for targeted queries
6. **Limit results**: Use LIMIT for exploratory queries
7. **Index awareness**: For Parquet, leverage row group statistics

## Monitoring

### Prometheus Metrics

```
# Query count by status
nebulaio_s3select_queries_total{status="success|error"}

# Query latency
nebulaio_s3select_query_duration_seconds{quantile="0.5|0.9|0.99"}

# Bytes scanned vs returned
nebulaio_s3select_bytes_scanned_total
nebulaio_s3select_bytes_returned_total

# Active queries
nebulaio_s3select_active_queries
```
