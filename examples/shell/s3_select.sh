#!/bin/bash
# NebulaIO S3 Select Shell Examples
#
# Prerequisites:
#   - AWS CLI v2 installed
#   - jq installed for JSON processing
#
# Configuration (required environment variables):
#   export NEBULAIO_ENDPOINT="https://localhost:9000"
#   export AWS_ACCESS_KEY_ID="admin"
#   export AWS_SECRET_ACCESS_KEY="your-secure-password"  # Required, min 12 chars

set -e

ENDPOINT="${NEBULAIO_ENDPOINT:-http://localhost:9000}"

# Helper function for S3 Select on CSV
select_csv() {
    local bucket="$1"
    local key="$2"
    local query="$3"
    local output_file="${4:-/dev/stdout}"

    aws s3api select-object-content \
        --endpoint-url "$ENDPOINT" \
        --bucket "$bucket" \
        --key "$key" \
        --expression "$query" \
        --expression-type SQL \
        --input-serialization '{"CSV": {"FileHeaderInfo": "USE", "FieldDelimiter": ","}}' \
        --output-serialization '{"JSON": {"RecordDelimiter": "\n"}}' \
        "$output_file"
}

# Helper function for S3 Select on JSON
select_json() {
    local bucket="$1"
    local key="$2"
    local query="$3"
    local output_file="${4:-/dev/stdout}"

    aws s3api select-object-content \
        --endpoint-url "$ENDPOINT" \
        --bucket "$bucket" \
        --key "$key" \
        --expression "$query" \
        --expression-type SQL \
        --input-serialization '{"JSON": {"Type": "LINES"}}' \
        --output-serialization '{"JSON": {"RecordDelimiter": "\n"}}' \
        "$output_file"
}

# Helper function for S3 Select on Parquet
select_parquet() {
    local bucket="$1"
    local key="$2"
    local query="$3"
    local output_file="${4:-/dev/stdout}"

    aws s3api select-object-content \
        --endpoint-url "$ENDPOINT" \
        --bucket "$bucket" \
        --key "$key" \
        --expression "$query" \
        --expression-type SQL \
        --input-serialization '{"Parquet": {}}' \
        --output-serialization '{"JSON": {"RecordDelimiter": "\n"}}' \
        "$output_file"
}

# Example: Query CSV file
example_csv_query() {
    echo "=== CSV Query Example ==="
    echo "Finding high-value transactions..."

    select_csv "analytics" "transactions/2024/01/data.csv" \
        "SELECT transaction_id, customer_id, amount FROM s3object s WHERE CAST(s.amount AS DECIMAL) > 10000"

    echo
}

# Example: Query JSON logs
example_json_query() {
    echo "=== JSON Query Example ==="
    echo "Finding error logs..."

    select_json "logs" "application/2024/01/01.json" \
        "SELECT s.timestamp, s.message, s.stack_trace FROM s3object s WHERE s.level = 'ERROR'"

    echo
}

# Example: Aggregate query on Parquet
example_parquet_aggregate() {
    echo "=== Parquet Aggregate Example ==="
    echo "Calculating regional sales totals..."

    select_parquet "data-warehouse" "sales/2024/q1.parquet" \
        "SELECT s.region, SUM(CAST(s.amount AS DECIMAL)) AS total FROM s3object s GROUP BY s.region"

    echo
}

# Example: Complex filter with multiple conditions
example_complex_filter() {
    echo "=== Complex Filter Example ==="
    echo "Finding specific user activity..."

    select_csv "events" "user-activity/2024/events.csv" \
        "SELECT s.user_id, s.event_type, s.timestamp FROM s3object s WHERE s.user_id = 'user-123' AND s.event_type IN ('login', 'purchase') AND s.timestamp > '2024-01-01T00:00:00Z'"

    echo
}

# Example: String manipulation
example_string_functions() {
    echo "=== String Function Example ==="
    echo "Processing names..."

    select_csv "customers" "records.csv" \
        "SELECT UPPER(s.first_name) AS first, LOWER(s.last_name) AS last, TRIM(s.email) AS email FROM s3object s WHERE s.email LIKE '%@example.com'"

    echo
}

# Main
case "${1:-help}" in
    csv)
        example_csv_query
        ;;
    json)
        example_json_query
        ;;
    parquet)
        example_parquet_aggregate
        ;;
    complex)
        example_complex_filter
        ;;
    string)
        example_string_functions
        ;;
    all)
        example_csv_query
        example_json_query
        example_parquet_aggregate
        example_complex_filter
        example_string_functions
        ;;
    custom)
        if [ $# -lt 4 ]; then
            echo "Usage: $0 custom <bucket> <key> <query> [format: csv|json|parquet]"
            exit 1
        fi
        bucket="$2"
        key="$3"
        query="$4"
        format="${5:-csv}"

        case "$format" in
            csv) select_csv "$bucket" "$key" "$query" ;;
            json) select_json "$bucket" "$key" "$query" ;;
            parquet) select_parquet "$bucket" "$key" "$query" ;;
        esac
        ;;
    *)
        echo "NebulaIO S3 Select Examples"
        echo
        echo "Usage: $0 <command>"
        echo
        echo "Commands:"
        echo "  csv       - Query CSV file example"
        echo "  json      - Query JSON file example"
        echo "  parquet   - Query Parquet file example"
        echo "  complex   - Complex filter example"
        echo "  string    - String function example"
        echo "  all       - Run all examples"
        echo "  custom    - Run custom query: $0 custom <bucket> <key> <query> [format]"
        echo
        echo "Environment:"
        echo "  NEBULAIO_ENDPOINT    - S3 endpoint (default: http://localhost:9000)"
        echo "  AWS_ACCESS_KEY_ID    - Access key"
        echo "  AWS_SECRET_ACCESS_KEY - Secret key"
        ;;
esac
