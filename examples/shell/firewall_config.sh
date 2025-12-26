#!/bin/bash
# NebulaIO Data Firewall Configuration Examples
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

# Get current firewall configuration
get_config() {
    echo "=== Current Firewall Configuration ==="
    api_call GET "/admin/firewall/config" | jq .
}

# Get firewall statistics
get_stats() {
    echo "=== Firewall Statistics ==="
    api_call GET "/admin/firewall/stats" | jq .
}

# Update rate limiting configuration
update_rate_limit() {
    local rps="${1:-1000}"
    local burst="${2:-100}"

    echo "Updating rate limit: $rps req/s, burst: $burst"
    api_call PUT "/admin/firewall/config/rate-limit" "{
        \"enabled\": true,
        \"requests_per_second\": $rps,
        \"burst_size\": $burst,
        \"per_user\": true,
        \"per_ip\": true
    }" | jq .
}

# Update bandwidth configuration
update_bandwidth() {
    local max_bps="${1:-1073741824}"  # 1 GB/s default
    local per_user="${2:-104857600}"   # 100 MB/s default

    echo "Updating bandwidth: max=$((max_bps / 1048576)) MB/s, per_user=$((per_user / 1048576)) MB/s"
    api_call PUT "/admin/firewall/config/bandwidth" "{
        \"enabled\": true,
        \"max_bytes_per_second\": $max_bps,
        \"max_bytes_per_second_per_user\": $per_user
    }" | jq .
}

# Add IP to allowlist
add_allowlist() {
    local ip="$1"
    echo "Adding $ip to allowlist"
    api_call POST "/admin/firewall/ip-allowlist" "{\"ip\": \"$ip\"}" | jq .
}

# Add IP to blocklist
add_blocklist() {
    local ip="$1"
    echo "Adding $ip to blocklist"
    api_call POST "/admin/firewall/ip-blocklist" "{\"ip\": \"$ip\"}" | jq .
}

# Remove from allowlist
remove_allowlist() {
    local ip="$1"
    echo "Removing $ip from allowlist"
    api_call DELETE "/admin/firewall/ip-allowlist/$ip" | jq .
}

# Remove from blocklist
remove_blocklist() {
    local ip="$1"
    echo "Removing $ip from blocklist"
    api_call DELETE "/admin/firewall/ip-blocklist/$ip" | jq .
}

# List IP rules
list_ip_rules() {
    echo "=== IP Allowlist ==="
    api_call GET "/admin/firewall/ip-allowlist" | jq -r '.[]'
    echo
    echo "=== IP Blocklist ==="
    api_call GET "/admin/firewall/ip-blocklist" | jq -r '.[]'
}

# Add a firewall rule
add_rule() {
    local rule_id="$1"
    local action="$2"
    local priority="${3:-100}"

    cat <<EOF
Adding rule: $rule_id
  Action: $action
  Priority: $priority
EOF

    api_call POST "/admin/firewall/rules" "{
        \"id\": \"$rule_id\",
        \"priority\": $priority,
        \"action\": \"$action\",
        \"enabled\": true
    }" | jq .
}

# List all rules
list_rules() {
    echo "=== Firewall Rules ==="
    api_call GET "/admin/firewall/rules" | jq '.[] | "\(.id)\t\(.priority)\t\(.action)\t\(.enabled)"'
}

# Delete a rule
delete_rule() {
    local rule_id="$1"
    echo "Deleting rule: $rule_id"
    api_call DELETE "/admin/firewall/rules/$rule_id"
    echo "Deleted"
}

# Set user-specific rate limit
set_user_rate_limit() {
    local user="$1"
    local rps="$2"

    echo "Setting rate limit for $user: $rps req/s"
    api_call PUT "/admin/firewall/user-limits/$user" "{
        \"requests_per_second\": $rps
    }" | jq .
}

# Set bucket-specific bandwidth limit
set_bucket_bandwidth() {
    local bucket="$1"
    local bps="$2"

    echo "Setting bandwidth for $bucket: $((bps / 1048576)) MB/s"
    api_call PUT "/admin/firewall/bucket-limits/$bucket" "{
        \"max_bytes_per_second\": $bps
    }" | jq .
}

# Example: Configure for production
configure_production() {
    echo "=== Configuring Production Firewall ==="
    echo

    # Rate limiting
    echo "1. Configuring rate limiting..."
    update_rate_limit 1000 100
    echo

    # Bandwidth
    echo "2. Configuring bandwidth..."
    update_bandwidth 1073741824 104857600
    echo

    # Connection limits
    echo "3. Configuring connection limits..."
    api_call PUT "/admin/firewall/config/connections" "{
        \"enabled\": true,
        \"max_connections\": 10000,
        \"max_connections_per_ip\": 100,
        \"max_connections_per_user\": 500
    }" | jq .
    echo

    echo "Production firewall configuration complete!"
}

# Example: Block large uploads
configure_block_large_uploads() {
    echo "=== Configuring Rule: Block Large Uploads ==="

    api_call POST "/admin/firewall/rules" '{
        "id": "block-large-uploads",
        "priority": 100,
        "action": "deny",
        "enabled": true,
        "match": {
            "operations": ["PutObject", "UploadPart"],
            "min_size": 1073741824
        },
        "description": "Block uploads larger than 1GB"
    }' | jq .
}

# Example: Restrict by time window
configure_maintenance_window() {
    echo "=== Configuring Rule: Maintenance Window ==="

    api_call POST "/admin/firewall/rules" '{
        "id": "maintenance-window",
        "priority": 50,
        "action": "deny",
        "enabled": true,
        "match": {
            "operations": ["PutObject", "DeleteObject"],
            "time_window": {
                "start": "02:00",
                "end": "04:00",
                "timezone": "UTC"
            }
        },
        "description": "Block write operations during maintenance window"
    }' | jq .
}

# Main
case "${1:-help}" in
    config)
        get_config
        ;;
    stats)
        get_stats
        ;;
    rate-limit)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 rate-limit <requests-per-second> [burst-size]"
            exit 1
        fi
        update_rate_limit "$2" "${3:-100}"
        ;;
    bandwidth)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 bandwidth <max-bytes-per-second> [per-user-bytes-per-second]"
            exit 1
        fi
        update_bandwidth "$2" "${3:-104857600}"
        ;;
    allow)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 allow <ip-or-cidr>"
            exit 1
        fi
        add_allowlist "$2"
        ;;
    block)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 block <ip-or-cidr>"
            exit 1
        fi
        add_blocklist "$2"
        ;;
    unallow)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 unallow <ip-or-cidr>"
            exit 1
        fi
        remove_allowlist "$2"
        ;;
    unblock)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 unblock <ip-or-cidr>"
            exit 1
        fi
        remove_blocklist "$2"
        ;;
    list-ips)
        list_ip_rules
        ;;
    add-rule)
        if [ $# -lt 3 ]; then
            echo "Usage: $0 add-rule <rule-id> <allow|deny> [priority]"
            exit 1
        fi
        add_rule "$2" "$3" "${4:-100}"
        ;;
    list-rules)
        list_rules
        ;;
    delete-rule)
        if [ $# -lt 2 ]; then
            echo "Usage: $0 delete-rule <rule-id>"
            exit 1
        fi
        delete_rule "$2"
        ;;
    user-limit)
        if [ $# -lt 3 ]; then
            echo "Usage: $0 user-limit <username> <requests-per-second>"
            exit 1
        fi
        set_user_rate_limit "$2" "$3"
        ;;
    bucket-limit)
        if [ $# -lt 3 ]; then
            echo "Usage: $0 bucket-limit <bucket> <bytes-per-second>"
            exit 1
        fi
        set_bucket_bandwidth "$2" "$3"
        ;;
    production)
        configure_production
        ;;
    block-large)
        configure_block_large_uploads
        ;;
    maintenance)
        configure_maintenance_window
        ;;
    *)
        echo "NebulaIO Data Firewall Configuration Examples"
        echo
        echo "Usage: $0 <command> [options]"
        echo
        echo "Configuration:"
        echo "  config        - Show current configuration"
        echo "  stats         - Show firewall statistics"
        echo "  rate-limit    - Update rate limiting"
        echo "  bandwidth     - Update bandwidth limits"
        echo
        echo "IP Management:"
        echo "  allow         - Add IP to allowlist"
        echo "  block         - Add IP to blocklist"
        echo "  unallow       - Remove IP from allowlist"
        echo "  unblock       - Remove IP from blocklist"
        echo "  list-ips      - List IP rules"
        echo
        echo "Rules:"
        echo "  add-rule      - Add a firewall rule"
        echo "  list-rules    - List all rules"
        echo "  delete-rule   - Delete a rule"
        echo
        echo "Per-Entity Limits:"
        echo "  user-limit    - Set user rate limit"
        echo "  bucket-limit  - Set bucket bandwidth limit"
        echo
        echo "Presets:"
        echo "  production    - Configure for production"
        echo "  block-large   - Block uploads >1GB"
        echo "  maintenance   - Configure maintenance window"
        echo
        echo "Environment:"
        echo "  NEBULAIO_ADMIN_ENDPOINT - Admin API endpoint"
        echo "  NEBULAIO_ADMIN_TOKEN    - Admin API token"
        ;;
esac
