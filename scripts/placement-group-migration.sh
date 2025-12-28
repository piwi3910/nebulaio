#!/bin/bash
#
# Placement Group Migration Tool for NebulaIO
#
# This script provides utilities for managing placement groups in production environments.
# It supports adding, removing, and migrating placement groups safely.
#
# Usage:
#   ./placement-group-migration.sh <command> [options]
#
# Commands:
#   status              Show current placement group status
#   list                List all placement groups
#   add-group           Add a new placement group
#   remove-group        Remove a placement group (must be empty)
#   add-node            Add a node to a placement group
#   remove-node         Remove a node from a placement group
#   drain-node          Gracefully drain a node before removal
#   rebalance           Trigger shard rebalancing within a group
#   validate-config     Validate placement group configuration
#   backup-config       Backup current configuration
#   restore-config      Restore configuration from backup
#
# Environment Variables:
#   NEBULAIO_API_URL    Base URL for NebulaIO admin API (default: http://localhost:8080)
#   NEBULAIO_API_TOKEN  Bearer token for authentication
#   NEBULAIO_CONFIG     Path to config file (default: /etc/nebulaio/config.yaml)
#

set -euo pipefail

# Configuration
NEBULAIO_API_URL="${NEBULAIO_API_URL:-http://localhost:8080}"
NEBULAIO_API_TOKEN="${NEBULAIO_API_TOKEN:-}"
NEBULAIO_CONFIG="${NEBULAIO_CONFIG:-/etc/nebulaio/config.yaml}"
BACKUP_DIR="${BACKUP_DIR:-/var/backups/nebulaio}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

check_dependencies() {
    local deps=("curl" "jq" "yq")
    for dep in "${deps[@]}"; do
        if ! command -v "$dep" &> /dev/null; then
            log_error "Required dependency '$dep' is not installed"
            exit 1
        fi
    done
}

check_auth() {
    if [[ -z "$NEBULAIO_API_TOKEN" ]]; then
        log_error "NEBULAIO_API_TOKEN environment variable is not set"
        log_info "Please set it with: export NEBULAIO_API_TOKEN='your-token'"
        exit 1
    fi
}

api_call() {
    local method="$1"
    local endpoint="$2"
    local data="${3:-}"

    local args=(-s -w "\n%{http_code}" -X "$method")
    args+=(-H "Authorization: Bearer $NEBULAIO_API_TOKEN")
    args+=(-H "Content-Type: application/json")

    if [[ -n "$data" ]]; then
        args+=(-d "$data")
    fi

    local response
    response=$(curl "${args[@]}" "${NEBULAIO_API_URL}${endpoint}")

    local http_code
    http_code=$(echo "$response" | tail -n 1)
    local body
    body=$(echo "$response" | sed '$d')

    if [[ "$http_code" -ge 400 ]]; then
        log_error "API call failed with status $http_code: $body"
        return 1
    fi

    echo "$body"
}

# Commands
cmd_status() {
    check_auth
    log_info "Fetching placement group status..."

    local response
    response=$(api_call GET "/api/v1/admin/cluster/placement-groups")

    echo ""
    echo "=== Placement Group Status ==="
    echo ""

    local total healthy degraded
    total=$(echo "$response" | jq -r '.total_groups // 0')
    healthy=$(echo "$response" | jq -r '.healthy_groups // 0')
    degraded=$(echo "$response" | jq -r '.degraded_groups // 0')
    local offline=$((total - healthy - degraded))

    echo "Total Groups:    $total"
    echo -e "Healthy Groups:  ${GREEN}$healthy${NC}"
    if [[ "$degraded" -gt 0 ]]; then
        echo -e "Degraded Groups: ${YELLOW}$degraded${NC}"
    fi
    if [[ "$offline" -gt 0 ]]; then
        echo -e "Offline Groups:  ${RED}$offline${NC}"
    fi
    echo ""

    local local_group
    local_group=$(echo "$response" | jq -r '.local_group_id // "N/A"')
    echo "Local Group ID: $local_group"
    echo ""
}

cmd_list() {
    check_auth
    log_info "Listing placement groups..."

    local response
    response=$(api_call GET "/api/v1/admin/cluster/placement-groups")

    echo ""
    echo "=== Placement Groups ==="
    echo ""

    echo "$response" | jq -r '
        .placement_groups[]? |
        "ID: \(.id)\n" +
        "  Name:       \(.name)\n" +
        "  Datacenter: \(.datacenter)\n" +
        "  Region:     \(.region)\n" +
        "  Status:     \(.status)\n" +
        "  Nodes:      \(.nodes | length) (min: \(.min_nodes), max: \(.max_nodes))\n" +
        "  Is Local:   \(.is_local)\n"
    '
}

cmd_add_group() {
    local id="${1:-}"
    local name="${2:-}"
    local datacenter="${3:-}"
    local region="${4:-}"
    local min_nodes="${5:-14}"
    local max_nodes="${6:-50}"

    if [[ -z "$id" || -z "$name" || -z "$datacenter" || -z "$region" ]]; then
        echo "Usage: $0 add-group <id> <name> <datacenter> <region> [min_nodes] [max_nodes]"
        echo ""
        echo "Example:"
        echo "  $0 add-group pg-dc3 'EU West Datacenter' dc3 eu-west-1 14 50"
        exit 1
    fi

    check_auth
    log_info "Adding placement group '$id'..."

    local data
    data=$(jq -n \
        --arg id "$id" \
        --arg name "$name" \
        --arg dc "$datacenter" \
        --arg region "$region" \
        --argjson min "$min_nodes" \
        --argjson max "$max_nodes" \
        '{
            id: $id,
            name: $name,
            datacenter: $dc,
            region: $region,
            min_nodes: $min,
            max_nodes: $max
        }')

    api_call POST "/api/v1/admin/cluster/placement-groups" "$data"
    log_success "Placement group '$id' created successfully"
}

cmd_remove_group() {
    local id="${1:-}"
    local force="${2:-false}"

    if [[ -z "$id" ]]; then
        echo "Usage: $0 remove-group <id> [--force]"
        exit 1
    fi

    check_auth

    # Check if group is empty
    local response
    response=$(api_call GET "/api/v1/admin/cluster/placement-groups/$id")

    local node_count
    node_count=$(echo "$response" | jq -r '.nodes | length')

    if [[ "$node_count" -gt 0 && "$force" != "--force" ]]; then
        log_error "Placement group '$id' has $node_count nodes. Use --force to remove anyway."
        log_warn "This will orphan the nodes! Consider draining first."
        exit 1
    fi

    log_warn "Removing placement group '$id'..."
    api_call DELETE "/api/v1/admin/cluster/placement-groups/$id"
    log_success "Placement group '$id' removed successfully"
}

cmd_add_node() {
    local group_id="${1:-}"
    local node_id="${2:-}"

    if [[ -z "$group_id" || -z "$node_id" ]]; then
        echo "Usage: $0 add-node <group_id> <node_id>"
        exit 1
    fi

    check_auth
    log_info "Adding node '$node_id' to placement group '$group_id'..."

    local data
    data=$(jq -n --arg node "$node_id" '{node_id: $node}')

    api_call POST "/api/v1/admin/cluster/placement-groups/$group_id/nodes" "$data"
    log_success "Node '$node_id' added to placement group '$group_id'"
}

cmd_remove_node() {
    local group_id="${1:-}"
    local node_id="${2:-}"

    if [[ -z "$group_id" || -z "$node_id" ]]; then
        echo "Usage: $0 remove-node <group_id> <node_id>"
        exit 1
    fi

    check_auth
    log_warn "Removing node '$node_id' from placement group '$group_id'..."

    api_call DELETE "/api/v1/admin/cluster/placement-groups/$group_id/nodes/$node_id"
    log_success "Node '$node_id' removed from placement group '$group_id'"
}

cmd_drain_node() {
    local group_id="${1:-}"
    local node_id="${2:-}"
    local timeout="${3:-300}"

    if [[ -z "$group_id" || -z "$node_id" ]]; then
        echo "Usage: $0 drain-node <group_id> <node_id> [timeout_seconds]"
        exit 1
    fi

    check_auth
    log_info "Draining node '$node_id' from placement group '$group_id'..."
    log_info "Timeout: ${timeout}s"

    local data
    data=$(jq -n --argjson timeout "$timeout" '{timeout_seconds: $timeout}')

    api_call POST "/api/v1/admin/cluster/placement-groups/$group_id/nodes/$node_id/drain" "$data"

    log_info "Waiting for drain to complete..."

    local start_time=$SECONDS
    while true; do
        local status
        status=$(api_call GET "/api/v1/admin/cluster/nodes/$node_id/status" | jq -r '.drain_status // "unknown"')

        if [[ "$status" == "drained" ]]; then
            log_success "Node '$node_id' drained successfully"
            break
        elif [[ "$status" == "failed" ]]; then
            log_error "Drain failed for node '$node_id'"
            exit 1
        fi

        local elapsed=$((SECONDS - start_time))
        if [[ "$elapsed" -ge "$timeout" ]]; then
            log_error "Drain timed out after ${timeout}s"
            exit 1
        fi

        log_info "Drain status: $status (${elapsed}s elapsed)"
        sleep 5
    done
}

cmd_rebalance() {
    local group_id="${1:-}"

    if [[ -z "$group_id" ]]; then
        echo "Usage: $0 rebalance <group_id>"
        exit 1
    fi

    check_auth
    log_info "Triggering rebalance for placement group '$group_id'..."

    api_call POST "/api/v1/admin/cluster/placement-groups/$group_id/rebalance" "{}"
    log_success "Rebalance triggered for placement group '$group_id'"
}

cmd_validate_config() {
    local config_file="${1:-$NEBULAIO_CONFIG}"

    if [[ ! -f "$config_file" ]]; then
        log_error "Configuration file not found: $config_file"
        exit 1
    fi

    log_info "Validating configuration: $config_file"

    # Check YAML syntax
    if ! yq eval '.' "$config_file" > /dev/null 2>&1; then
        log_error "Invalid YAML syntax"
        exit 1
    fi

    # Extract placement group configuration
    local groups
    groups=$(yq eval '.storage.placement_groups.groups // []' "$config_file")

    local data_shards parity_shards
    data_shards=$(yq eval '.storage.default_redundancy.data_shards // 10' "$config_file")
    parity_shards=$(yq eval '.storage.default_redundancy.parity_shards // 4' "$config_file")
    local total_shards=$((data_shards + parity_shards))

    log_info "Redundancy configuration: $data_shards data + $parity_shards parity = $total_shards total shards"

    local errors=0
    local group_count
    group_count=$(echo "$groups" | yq eval 'length' -)

    if [[ "$group_count" -eq 0 ]]; then
        log_warn "No placement groups configured"
    else
        log_info "Validating $group_count placement group(s)..."

        for i in $(seq 0 $((group_count - 1))); do
            local id name min_nodes max_nodes
            id=$(echo "$groups" | yq eval ".[$i].id // \"\"" -)
            name=$(echo "$groups" | yq eval ".[$i].name // \"\"" -)
            min_nodes=$(echo "$groups" | yq eval ".[$i].min_nodes // 0" -)
            max_nodes=$(echo "$groups" | yq eval ".[$i].max_nodes // 0" -)

            echo ""
            log_info "Group: $id ($name)"

            if [[ -z "$id" ]]; then
                log_error "  - Missing group ID"
                ((errors++))
            fi

            if [[ "$min_nodes" -lt "$total_shards" ]]; then
                log_error "  - min_nodes ($min_nodes) < total_shards ($total_shards)"
                ((errors++))
            else
                log_success "  - min_nodes ($min_nodes) >= total_shards ($total_shards)"
            fi

            if [[ "$max_nodes" -gt 0 && "$max_nodes" -lt "$min_nodes" ]]; then
                log_error "  - max_nodes ($max_nodes) < min_nodes ($min_nodes)"
                ((errors++))
            fi
        done
    fi

    echo ""
    if [[ "$errors" -gt 0 ]]; then
        log_error "Validation failed with $errors error(s)"
        exit 1
    else
        log_success "Configuration is valid"
    fi
}

cmd_backup_config() {
    local config_file="${1:-$NEBULAIO_CONFIG}"

    if [[ ! -f "$config_file" ]]; then
        log_error "Configuration file not found: $config_file"
        exit 1
    fi

    mkdir -p "$BACKUP_DIR"

    local timestamp
    timestamp=$(date +%Y%m%d_%H%M%S)
    local backup_file="$BACKUP_DIR/config_${timestamp}.yaml"

    cp "$config_file" "$backup_file"
    log_success "Configuration backed up to: $backup_file"
}

cmd_restore_config() {
    local backup_file="${1:-}"
    local config_file="${2:-$NEBULAIO_CONFIG}"

    if [[ -z "$backup_file" ]]; then
        echo "Usage: $0 restore-config <backup_file> [config_file]"
        echo ""
        echo "Available backups:"
        ls -la "$BACKUP_DIR"/*.yaml 2>/dev/null || echo "  No backups found"
        exit 1
    fi

    if [[ ! -f "$backup_file" ]]; then
        log_error "Backup file not found: $backup_file"
        exit 1
    fi

    log_warn "This will overwrite: $config_file"
    read -p "Continue? [y/N] " -n 1 -r
    echo

    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        log_info "Cancelled"
        exit 0
    fi

    # Backup current config first
    cmd_backup_config "$config_file"

    cp "$backup_file" "$config_file"
    log_success "Configuration restored from: $backup_file"
    log_info "Restart NebulaIO for changes to take effect"
}

show_usage() {
    echo "NebulaIO Placement Group Migration Tool"
    echo ""
    echo "Usage: $0 <command> [options]"
    echo ""
    echo "Commands:"
    echo "  status              Show current placement group status"
    echo "  list                List all placement groups"
    echo "  add-group           Add a new placement group"
    echo "  remove-group        Remove a placement group"
    echo "  add-node            Add a node to a placement group"
    echo "  remove-node         Remove a node from a placement group"
    echo "  drain-node          Gracefully drain a node before removal"
    echo "  rebalance           Trigger shard rebalancing within a group"
    echo "  validate-config     Validate placement group configuration"
    echo "  backup-config       Backup current configuration"
    echo "  restore-config      Restore configuration from backup"
    echo ""
    echo "Environment Variables:"
    echo "  NEBULAIO_API_URL    Base URL for NebulaIO admin API"
    echo "  NEBULAIO_API_TOKEN  Bearer token for authentication"
    echo "  NEBULAIO_CONFIG     Path to config file"
    echo ""
    echo "Examples:"
    echo "  $0 status"
    echo "  $0 list"
    echo "  $0 add-group pg-dc3 'EU West Datacenter' dc3 eu-west-1 14 50"
    echo "  $0 add-node pg-dc1 node-abc123"
    echo "  $0 drain-node pg-dc1 node-abc123 600"
    echo "  $0 validate-config /path/to/config.yaml"
}

# Main
main() {
    local command="${1:-help}"
    shift || true

    case "$command" in
        status)
            check_dependencies
            cmd_status "$@"
            ;;
        list)
            check_dependencies
            cmd_list "$@"
            ;;
        add-group)
            check_dependencies
            cmd_add_group "$@"
            ;;
        remove-group)
            check_dependencies
            cmd_remove_group "$@"
            ;;
        add-node)
            check_dependencies
            cmd_add_node "$@"
            ;;
        remove-node)
            check_dependencies
            cmd_remove_node "$@"
            ;;
        drain-node)
            check_dependencies
            cmd_drain_node "$@"
            ;;
        rebalance)
            check_dependencies
            cmd_rebalance "$@"
            ;;
        validate-config)
            check_dependencies
            cmd_validate_config "$@"
            ;;
        backup-config)
            cmd_backup_config "$@"
            ;;
        restore-config)
            cmd_restore_config "$@"
            ;;
        help|--help|-h)
            show_usage
            ;;
        *)
            log_error "Unknown command: $command"
            show_usage
            exit 1
            ;;
    esac
}

main "$@"
