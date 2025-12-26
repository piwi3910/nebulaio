#!/bin/bash
# NebulaIO Batch Replication Shell Examples
#
# Prerequisites:
#   - curl installed
#   - jq installed for JSON processing
#
# Configuration:
#   export NEBULAIO_ADMIN_ENDPOINT="http://localhost:9001"
#   export NEBULAIO_ADMIN_TOKEN="your-admin-token"

set -e

ADMIN_ENDPOINT="${NEBULAIO_ADMIN_ENDPOINT:-http://localhost:9001}"
ADMIN_TOKEN="${NEBULAIO_ADMIN_TOKEN:-}"

# Helper function for API calls
api_call() {
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

# Create a new batch replication job
create_job() {
    local job_id="$1"
    local source_bucket="$2"
    local dest_bucket="$3"
    local description="${4:-Batch replication job}"
    local concurrency="${5:-10}"

    echo "Creating job: $job_id"
    api_call POST "/admin/batch/jobs" "{
        \"job_id\": \"$job_id\",
        \"source_bucket\": \"$source_bucket\",
        \"destination_bucket\": \"$dest_bucket\",
        \"description\": \"$description\",
        \"concurrency\": $concurrency
    }" | jq .
}

# Create a cross-cluster replication job
create_cross_cluster_job() {
    local job_id="$1"
    local source_bucket="$2"
    local dest_bucket="$3"
    local dest_endpoint="$4"
    local dest_access_key="$5"
    local dest_secret_key="$6"
    local rate_limit="${7:-0}"

    echo "Creating cross-cluster job: $job_id"

    local data="{
        \"job_id\": \"$job_id\",
        \"source_bucket\": \"$source_bucket\",
        \"destination_bucket\": \"$dest_bucket\",
        \"destination_endpoint\": \"$dest_endpoint\",
        \"destination_access_key\": \"$dest_access_key\",
        \"destination_secret_key\": \"$dest_secret_key\",
        \"description\": \"Cross-cluster replication to $dest_endpoint\"
    }"

    if [ "$rate_limit" -gt 0 ]; then
        data=$(echo "$data" | jq ". + {\"rate_limit_bytes_per_sec\": $rate_limit}")
    fi

    api_call POST "/admin/batch/jobs" "$data" | jq .
}

# Start a job
start_job() {
    local job_id="$1"
    echo "Starting job: $job_id"
    api_call POST "/admin/batch/jobs/$job_id/start" | jq .
}

# Get job status
get_status() {
    local job_id="$1"
    api_call GET "/admin/batch/jobs/$job_id" | jq .
}

# List all jobs
list_jobs() {
    api_call GET "/admin/batch/jobs" | jq -r '.[] | "\(.job_id)\t\(.status)\t\(.source_bucket) -> \(.destination_bucket)"'
}

# Pause a job
pause_job() {
    local job_id="$1"
    echo "Pausing job: $job_id"
    api_call POST "/admin/batch/jobs/$job_id/pause" | jq .
}

# Resume a job
resume_job() {
    local job_id="$1"
    echo "Resuming job: $job_id"
    api_call POST "/admin/batch/jobs/$job_id/resume" | jq .
}

# Cancel a job
cancel_job() {
    local job_id="$1"
    echo "Cancelling job: $job_id"
    api_call POST "/admin/batch/jobs/$job_id/cancel" | jq .
}

# Delete a job
delete_job() {
    local job_id="$1"
    echo "Deleting job: $job_id"
    api_call DELETE "/admin/batch/jobs/$job_id"
    echo "Deleted"
}

# Get failed objects
get_failed() {
    local job_id="$1"
    api_call GET "/admin/batch/jobs/$job_id/failed" | jq .
}

# Retry failed objects
retry_failed() {
    local job_id="$1"
    echo "Retrying failed objects for job: $job_id"
    api_call POST "/admin/batch/jobs/$job_id/retry-failed" | jq .
}

# Monitor job progress
monitor_job() {
    local job_id="$1"
    local interval="${2:-5}"

    echo "Monitoring job: $job_id (Ctrl+C to stop)"
    echo

    while true; do
        status=$(api_call GET "/admin/batch/jobs/$job_id")

        state=$(echo "$status" | jq -r '.status')
        total_obj=$(echo "$status" | jq -r '.progress.total_objects // 0')
        done_obj=$(echo "$status" | jq -r '.progress.completed_objects // 0')
        failed_obj=$(echo "$status" | jq -r '.progress.failed_objects // 0')
        total_bytes=$(echo "$status" | jq -r '.progress.total_bytes // 0')
        done_bytes=$(echo "$status" | jq -r '.progress.completed_bytes // 0')
        rate=$(echo "$status" | jq -r '.progress.current_rate_bytes_sec // 0')

        # Calculate percentage
        if [ "$total_obj" -gt 0 ]; then
            pct=$((done_obj * 100 / total_obj))
        else
            pct=0
        fi

        # Format bytes
        format_bytes() {
            local bytes=$1
            if [ "$bytes" -ge 1073741824 ]; then
                echo "$((bytes / 1073741824)) GB"
            elif [ "$bytes" -ge 1048576 ]; then
                echo "$((bytes / 1048576)) MB"
            elif [ "$bytes" -ge 1024 ]; then
                echo "$((bytes / 1024)) KB"
            else
                echo "$bytes B"
            fi
        }

        done_bytes_fmt=$(format_bytes "$done_bytes")
        total_bytes_fmt=$(format_bytes "$total_bytes")
        rate_fmt=$(format_bytes "$rate")

        printf "\r[%-10s] Objects: %d/%d (%d%%) | Bytes: %s/%s | Rate: %s/s | Failed: %d    " \
            "$state" "$done_obj" "$total_obj" "$pct" "$done_bytes_fmt" "$total_bytes_fmt" "$rate_fmt" "$failed_obj"

        if [ "$state" = "completed" ] || [ "$state" = "failed" ] || [ "$state" = "cancelled" ]; then
            echo
            break
        fi

        sleep "$interval"
    done
}

# Main
case "${1:-help}" in
    create)
        if [ $# -lt 4 ]; then
            echo "Usage: $0 create <job-id> <source-bucket> <dest-bucket> [description] [concurrency]"
            exit 1
        fi
        create_job "$2" "$3" "$4" "${5:-}" "${6:-10}"
        ;;
    create-cross)
        if [ $# -lt 7 ]; then
            echo "Usage: $0 create-cross <job-id> <source-bucket> <dest-bucket> <dest-endpoint> <access-key> <secret-key> [rate-limit-bytes]"
            exit 1
        fi
        create_cross_cluster_job "$2" "$3" "$4" "$5" "$6" "$7" "${8:-0}"
        ;;
    start)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 start <job-id>"
            exit 1
        fi
        start_job "$2"
        ;;
    status)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 status <job-id>"
            exit 1
        fi
        get_status "$2"
        ;;
    list)
        list_jobs
        ;;
    pause)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 pause <job-id>"
            exit 1
        fi
        pause_job "$2"
        ;;
    resume)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 resume <job-id>"
            exit 1
        fi
        resume_job "$2"
        ;;
    cancel)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 cancel <job-id>"
            exit 1
        fi
        cancel_job "$2"
        ;;
    delete)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 delete <job-id>"
            exit 1
        fi
        delete_job "$2"
        ;;
    failed)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 failed <job-id>"
            exit 1
        fi
        get_failed "$2"
        ;;
    retry)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 retry <job-id>"
            exit 1
        fi
        retry_failed "$2"
        ;;
    monitor)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 monitor <job-id> [interval-seconds]"
            exit 1
        fi
        monitor_job "$2" "${3:-5}"
        ;;
    run)
        # Create and start a job, then monitor
        if [ $# -lt 4 ]; then
            echo "Usage: $0 run <job-id> <source-bucket> <dest-bucket>"
            exit 1
        fi
        create_job "$2" "$3" "$4"
        start_job "$2"
        monitor_job "$2"
        ;;
    *)
        echo "NebulaIO Batch Replication Examples"
        echo
        echo "Usage: $0 <command> [options]"
        echo
        echo "Commands:"
        echo "  create      - Create a new job"
        echo "  create-cross - Create a cross-cluster job"
        echo "  start       - Start a job"
        echo "  status      - Get job status"
        echo "  list        - List all jobs"
        echo "  pause       - Pause a job"
        echo "  resume      - Resume a job"
        echo "  cancel      - Cancel a job"
        echo "  delete      - Delete a job"
        echo "  failed      - Get failed objects"
        echo "  retry       - Retry failed objects"
        echo "  monitor     - Monitor job progress"
        echo "  run         - Create, start, and monitor a job"
        echo
        echo "Environment:"
        echo "  NEBULAIO_ADMIN_ENDPOINT - Admin API endpoint (default: http://localhost:9001)"
        echo "  NEBULAIO_ADMIN_TOKEN    - Admin API token"
        ;;
esac
