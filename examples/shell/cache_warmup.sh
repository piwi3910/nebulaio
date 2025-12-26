#!/bin/bash
# NebulaIO DRAM Cache Warmup Script
#
# Pre-populate the cache with frequently accessed objects
# before high-load periods (e.g., ML training jobs)
#
# Prerequisites:
#   - curl installed
#   - jq installed for JSON processing
#   - AWS CLI v2 (optional, for S3 operations)
#
# Configuration:
#   export NEBULAIO_ENDPOINT="http://localhost:9000"
#   export NEBULAIO_ADMIN_ENDPOINT="http://localhost:9001"
#   export NEBULAIO_ADMIN_TOKEN="your-admin-token"
#   export AWS_ACCESS_KEY_ID="admin"
#   export AWS_SECRET_ACCESS_KEY="admin123"

set -e

ENDPOINT="${NEBULAIO_ENDPOINT:-http://localhost:9000}"
ADMIN_ENDPOINT="${NEBULAIO_ADMIN_ENDPOINT:-http://localhost:9001}"
ADMIN_TOKEN="${NEBULAIO_ADMIN_TOKEN:-}"

# Helper function for Admin API calls
admin_call() {
    local method="$1"
    local path="$2"
    local data="$3"

    if [ -n "$data" ]; then
        curl -s -X "$method" \
            "${ADMIN_ENDPOINT}${path}" \
            -H "Authorization: Bearer $ADMIN_TOKEN" \
            -H "Content-Type: application/json" \
            -d "$data"
    else
        curl -s -X "$method" \
            "${ADMIN_ENDPOINT}${path}" \
            -H "Authorization: Bearer $ADMIN_TOKEN"
    fi
}

# Get cache statistics
get_stats() {
    echo "=== Cache Statistics ==="
    local stats=$(admin_call GET "/admin/cache/stats")

    local enabled=$(echo "$stats" | jq -r '.enabled')
    local size=$(echo "$stats" | jq -r '.size')
    local max_size=$(echo "$stats" | jq -r '.max_size')
    local hit_count=$(echo "$stats" | jq -r '.hit_count')
    local miss_count=$(echo "$stats" | jq -r '.miss_count')
    local hit_rate=$(echo "$stats" | jq -r '.hit_rate')
    local prefetch_hits=$(echo "$stats" | jq -r '.prefetch_hits')
    local avg_latency=$(echo "$stats" | jq -r '.avg_latency_us')

    echo "Enabled: $enabled"
    echo "Size: $((size / 1048576)) MB / $((max_size / 1048576)) MB ($((size * 100 / max_size))%)"
    echo "Hit Rate: $(echo "$hit_rate * 100" | bc)%"
    echo "Hits: $hit_count"
    echo "Misses: $miss_count"
    echo "Prefetch Hits: $prefetch_hits"
    echo "Avg Latency: ${avg_latency} us"
    echo
}

# Warm cache with a specific bucket and prefix
warmup_prefix() {
    local bucket="$1"
    local prefix="$2"
    local recursive="${3:-true}"

    echo "Warming up cache: s3://$bucket/$prefix"
    admin_call POST "/admin/cache/warm" "{
        \"bucket\": \"$bucket\",
        \"prefix\": \"$prefix\",
        \"recursive\": $recursive
    }" | jq .
}

# Warm cache with a list of keys
warmup_keys() {
    local bucket="$1"
    shift
    local keys=("$@")

    echo "Warming up ${#keys[@]} specific keys..."

    local keys_json=$(printf '%s\n' "${keys[@]}" | jq -R . | jq -s .)

    admin_call POST "/admin/cache/warm" "{
        \"bucket\": \"$bucket\",
        \"keys\": $keys_json
    }" | jq .
}

# Invalidate cache entries
invalidate() {
    local bucket="$1"
    local prefix="${2:-}"

    if [ -n "$prefix" ]; then
        echo "Invalidating cache: s3://$bucket/$prefix"
        admin_call DELETE "/admin/cache/invalidate?bucket=$bucket&prefix=$prefix"
    else
        echo "Invalidating cache for bucket: $bucket"
        admin_call DELETE "/admin/cache/invalidate?bucket=$bucket"
    fi
    echo "Done"
}

# Clear entire cache
clear_cache() {
    echo "Clearing entire cache..."
    admin_call DELETE "/admin/cache/clear"
    echo "Cache cleared"
}

# Warmup for ML training
warmup_ml_training() {
    local bucket="$1"
    local dataset_prefix="${2:-training/}"
    local epochs="${3:-1}"

    echo "=== ML Training Cache Warmup ==="
    echo "Bucket: $bucket"
    echo "Dataset: $dataset_prefix"
    echo "Preparing for $epochs epoch(s)"
    echo

    # Get initial stats
    get_stats

    # List objects in the dataset
    echo "Listing objects..."
    local object_count=$(aws s3api list-objects-v2 \
        --endpoint-url "$ENDPOINT" \
        --bucket "$bucket" \
        --prefix "$dataset_prefix" \
        --query 'KeyCount' \
        --output text 2>/dev/null || echo "0")

    echo "Found $object_count objects in dataset"
    echo

    # Warm up the first epoch
    echo "Warming up epoch 1 data..."
    warmup_prefix "$bucket" "$dataset_prefix"

    # Wait for warmup to complete
    echo "Waiting for warmup to complete..."
    sleep 5

    # Show final stats
    get_stats
}

# Warmup from a manifest file
warmup_from_manifest() {
    local manifest_file="$1"
    local bucket="$2"

    if [ ! -f "$manifest_file" ]; then
        echo "Error: Manifest file not found: $manifest_file"
        exit 1
    fi

    echo "Warming up from manifest: $manifest_file"

    # Read keys from manifest (one per line)
    local keys=()
    while IFS= read -r key; do
        [ -n "$key" ] && keys+=("$key")
    done < "$manifest_file"

    echo "Found ${#keys[@]} keys in manifest"
    warmup_keys "$bucket" "${keys[@]}"
}

# Schedule warmup (for use with cron)
schedule_warmup() {
    local bucket="$1"
    local prefix="$2"
    local schedule="$3"

    echo "To schedule cache warmup, add this to your crontab:"
    echo
    echo "$schedule $0 prefix $bucket $prefix"
    echo
    echo "Example: Warm up at 6 AM daily"
    echo "0 6 * * * $0 prefix $bucket $prefix"
    echo
    echo "Example: Warm up before training at 2 AM on weekdays"
    echo "0 2 * * 1-5 $0 ml $bucket $prefix"
}

# Main
case "${1:-help}" in
    stats)
        get_stats
        ;;
    prefix)
        if [ $# -lt 3 ]; then
            echo "Usage: $0 prefix <bucket> <prefix> [recursive: true|false]"
            exit 1
        fi
        warmup_prefix "$2" "$3" "${4:-true}"
        ;;
    keys)
        if [ $# -lt 3 ]; then
            echo "Usage: $0 keys <bucket> <key1> [key2] [key3] ..."
            exit 1
        fi
        bucket="$2"
        shift 2
        warmup_keys "$bucket" "$@"
        ;;
    manifest)
        if [ $# -lt 3 ]; then
            echo "Usage: $0 manifest <manifest-file> <bucket>"
            exit 1
        fi
        warmup_from_manifest "$2" "$3"
        ;;
    ml)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 ml <bucket> [prefix] [epochs]"
            exit 1
        fi
        warmup_ml_training "$2" "${3:-training/}" "${4:-1}"
        ;;
    invalidate)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 invalidate <bucket> [prefix]"
            exit 1
        fi
        invalidate "$2" "${3:-}"
        ;;
    clear)
        clear_cache
        ;;
    schedule)
        if [ $# -lt 4 ]; then
            echo "Usage: $0 schedule <bucket> <prefix> <cron-schedule>"
            echo "Example: $0 schedule ml-data training/ '0 6 * * *'"
            exit 1
        fi
        schedule_warmup "$2" "$3" "$4"
        ;;
    *)
        echo "NebulaIO DRAM Cache Warmup Script"
        echo
        echo "Usage: $0 <command> [options]"
        echo
        echo "Commands:"
        echo "  stats       - Show cache statistics"
        echo "  prefix      - Warm up objects by prefix"
        echo "  keys        - Warm up specific keys"
        echo "  manifest    - Warm up from manifest file"
        echo "  ml          - ML training warmup workflow"
        echo "  invalidate  - Invalidate cache entries"
        echo "  clear       - Clear entire cache"
        echo "  schedule    - Show cron schedule examples"
        echo
        echo "Environment:"
        echo "  NEBULAIO_ENDPOINT       - S3 endpoint"
        echo "  NEBULAIO_ADMIN_ENDPOINT - Admin API endpoint"
        echo "  NEBULAIO_ADMIN_TOKEN    - Admin API token"
        echo "  AWS_ACCESS_KEY_ID       - Access key"
        echo "  AWS_SECRET_ACCESS_KEY   - Secret key"
        echo
        echo "Examples:"
        echo "  $0 stats"
        echo "  $0 prefix my-bucket training/epoch-1/"
        echo "  $0 ml my-ml-bucket training/"
        echo "  $0 invalidate my-bucket old-data/"
        ;;
esac
