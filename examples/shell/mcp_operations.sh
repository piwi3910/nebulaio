#!/bin/bash
#
# MCP Server Operations
#
# Shell examples for interacting with NebulaIO's MCP Server

set -e

# Configuration
MCP_ENDPOINT="${NEBULAIO_MCP_ENDPOINT:-http://localhost:9005/mcp}"
ACCESS_KEY="${NEBULAIO_ACCESS_KEY:-minioadmin}"
SECRET_KEY="${NEBULAIO_SECRET_KEY:-minioadmin}"

# Auth header
AUTH="Bearer ${ACCESS_KEY}:${SECRET_KEY}"

# JSON-RPC helper function
mcp_request() {
    local method="$1"
    local params="$2"

    curl -s -X POST "$MCP_ENDPOINT" \
        -H "Content-Type: application/json" \
        -H "Authorization: $AUTH" \
        -d "{
            \"jsonrpc\": \"2.0\",
            \"id\": $(date +%s),
            \"method\": \"$method\",
            \"params\": $params
        }" | jq .
}

echo "MCP Server Operations"
echo "====================="

# List available tools
echo ""
echo "1. List available tools:"
mcp_request "tools/list" "{}"

# List buckets
echo ""
echo "2. List buckets:"
mcp_request "tools/call" '{
    "name": "list_buckets",
    "arguments": {}
}'

# Create test bucket and upload data
echo ""
echo "3. Setup test data..."
BUCKET="mcp-shell-demo"

# Use S3 API to create bucket
aws --endpoint-url "http://localhost:9000" s3 mb "s3://$BUCKET" 2>/dev/null || true

# Upload test file
echo "This is a test document for MCP operations." | \
    aws --endpoint-url "http://localhost:9000" s3 cp - "s3://$BUCKET/test/document.txt"

echo "   Created $BUCKET with test document"

# List objects in bucket
echo ""
echo "4. List objects in bucket:"
mcp_request "tools/call" "{
    \"name\": \"list_objects\",
    \"arguments\": {
        \"bucket\": \"$BUCKET\",
        \"prefix\": \"test/\"
    }
}"

# Read object content
echo ""
echo "5. Read object content:"
mcp_request "tools/call" "{
    \"name\": \"read_object\",
    \"arguments\": {
        \"bucket\": \"$BUCKET\",
        \"key\": \"test/document.txt\"
    }
}"

# Write new object
echo ""
echo "6. Write new object via MCP:"
mcp_request "tools/call" "{
    \"name\": \"write_object\",
    \"arguments\": {
        \"bucket\": \"$BUCKET\",
        \"key\": \"test/output.txt\",
        \"content\": \"Written via MCP at $(date)\",
        \"content_type\": \"text/plain\"
    }
}"

# Get object info
echo ""
echo "7. Get object metadata:"
mcp_request "tools/call" "{
    \"name\": \"get_object_info\",
    \"arguments\": {
        \"bucket\": \"$BUCKET\",
        \"key\": \"test/output.txt\"
    }
}"

# List resources
echo ""
echo "8. List available resources:"
mcp_request "resources/list" "{}"

# Read resource by URI
echo ""
echo "9. Read resource by URI:"
mcp_request "resources/read" "{
    \"uri\": \"s3://$BUCKET/test/document.txt\"
}"

# Copy object
echo ""
echo "10. Copy object:"
mcp_request "tools/call" "{
    \"name\": \"copy_object\",
    \"arguments\": {
        \"source_bucket\": \"$BUCKET\",
        \"source_key\": \"test/document.txt\",
        \"dest_bucket\": \"$BUCKET\",
        \"dest_key\": \"test/document-copy.txt\"
    }
}"

# Delete object
echo ""
echo "11. Delete object:"
mcp_request "tools/call" "{
    \"name\": \"delete_object\",
    \"arguments\": {
        \"bucket\": \"$BUCKET\",
        \"key\": \"test/document-copy.txt\"
    }
}"

echo ""
echo "Done!"
echo ""
echo "MCP Endpoint: $MCP_ENDPOINT"
echo "Test Bucket: $BUCKET"
