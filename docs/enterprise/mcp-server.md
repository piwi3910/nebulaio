# MCP Server (Model Context Protocol)

NebulaIO includes a built-in MCP Server that enables AI agents like Claude, ChatGPT, and other LLMs to interact with your object storage directly.

## Overview

The Model Context Protocol (MCP) is an open standard for AI agent integration. NebulaIO's MCP Server provides:

- **Tools** - Read, write, list, and delete objects
- **Resources** - Browse bucket contents as resources
- **Prompts** - Pre-built prompts for common operations
- **Authentication** - Secure access with S3 credentials

## Features

### Tool Execution

AI agents can execute storage operations:

```json
{
  "jsonrpc": "2.0",
  "method": "tools/call",
  "params": {
    "name": "read_object",
    "arguments": {
      "bucket": "my-bucket",
      "key": "data/report.json"
    }
  }
}
```

### Resource Access

Browse and read bucket contents:

```json
{
  "jsonrpc": "2.0",
  "method": "resources/read",
  "params": {
    "uri": "s3://my-bucket/data/report.json"
  }
}
```

### Available Tools

| Tool | Description |
|------|-------------|
| `list_buckets` | List all accessible buckets |
| `list_objects` | List objects in a bucket |
| `read_object` | Read object content |
| `write_object` | Write/upload an object |
| `delete_object` | Delete an object |
| `copy_object` | Copy an object |
| `get_object_info` | Get object metadata |

## Configuration

### Basic Setup

```yaml
mcp:
  enabled: true
  port: 9005
  enable_tools: true
  enable_resources: true
  enable_prompts: true
```

### Advanced Configuration

```yaml
mcp:
  enabled: true
  port: 9005
  enable_tools: true
  enable_resources: true
  enable_prompts: true
  max_resource_size: 10485760  # 10MB max resource size
  allowed_buckets: []          # Empty = all buckets
  auth_required: true          # Require authentication
  rate_limit: 100              # Requests per minute per client
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NEBULAIO_MCP_ENABLED` | Enable MCP Server | `false` |
| `NEBULAIO_MCP_PORT` | MCP Server port | `9005` |

## Client Configuration

### Claude Desktop

Add to your Claude Desktop MCP configuration:

```json
{
  "mcpServers": {
    "nebulaio": {
      "command": "curl",
      "args": [
        "-X", "POST",
        "-H", "Content-Type: application/json",
        "-H", "Authorization: Bearer YOUR_ACCESS_KEY:YOUR_SECRET_KEY",
        "http://localhost:9005/mcp"
      ]
    }
  }
}
```

### Using MCP Client Libraries

```typescript
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const transport = new StreamableHTTPClientTransport(
  new URL("http://localhost:9005/mcp")
);

const client = new Client({
  name: "my-app",
  version: "1.0.0"
});

await client.connect(transport);

// List available tools
const tools = await client.listTools();

// Call a tool
const result = await client.callTool("list_buckets", {});
```

### Python MCP Client

```python
from mcp import ClientSession
from mcp.client.http import HTTPClient

async with HTTPClient("http://localhost:9005/mcp") as client:
    async with ClientSession(client) as session:
        # List tools
        tools = await session.list_tools()

        # Read an object
        result = await session.call_tool(
            "read_object",
            arguments={
                "bucket": "my-bucket",
                "key": "data.json"
            }
        )
        print(result.content)
```

## Usage Examples

### List Buckets

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "list_buckets",
    "arguments": {}
  }
}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Buckets:\n- my-bucket\n- logs-bucket\n- data-warehouse"
      }
    ]
  }
}
```

### Read Object

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "read_object",
    "arguments": {
      "bucket": "my-bucket",
      "key": "config.yaml"
    }
  }
}
```

### Write Object

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "tools/call",
  "params": {
    "name": "write_object",
    "arguments": {
      "bucket": "my-bucket",
      "key": "output/result.json",
      "content": "{\"status\": \"success\", \"data\": [1, 2, 3]}",
      "content_type": "application/json"
    }
  }
}
```

### List Objects with Prefix

```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "tools/call",
  "params": {
    "name": "list_objects",
    "arguments": {
      "bucket": "my-bucket",
      "prefix": "data/",
      "max_keys": 100
    }
  }
}
```

## Resources

### Browse Resources

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "resources/list",
  "params": {}
}
```

### Read Resource

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "resources/read",
  "params": {
    "uri": "s3://my-bucket/data/report.csv"
  }
}
```

## Security

### Authentication

Enable authentication for production:

```yaml
mcp:
  enabled: true
  auth_required: true
```

Include credentials in requests:

```bash
curl -X POST http://localhost:9005/mcp \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ACCESS_KEY:SECRET_KEY" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

### Bucket Restrictions

Limit access to specific buckets:

```yaml
mcp:
  enabled: true
  allowed_buckets:
    - "public-data"
    - "shared-resources"
```

### Rate Limiting

```yaml
mcp:
  enabled: true
  rate_limit: 60  # 60 requests per minute
```

## Integration with AI Workflows

### Claude Assistant Example

When using Claude with MCP, the assistant can:

1. List available data in buckets
2. Read configuration files
3. Analyze data files (CSV, JSON)
4. Write results back to storage

### LangChain Integration

```python
from langchain.tools import Tool
from mcp import ClientSession

async def read_s3_object(bucket: str, key: str) -> str:
    async with mcp_session as session:
        result = await session.call_tool(
            "read_object",
            arguments={"bucket": bucket, "key": key}
        )
        return result.content[0].text

s3_read_tool = Tool(
    name="read_s3_object",
    description="Read an object from S3 storage",
    func=read_s3_object
)
```

## Troubleshooting

### Common Issues

1. **Connection refused**: Ensure MCP is enabled and port 9005 is accessible
2. **Authentication failed**: Check credentials format `ACCESS_KEY:SECRET_KEY`
3. **Resource too large**: Increase `max_resource_size` or use streaming

### Logs

Enable debug logging:

```yaml
log_level: debug
```

Look for logs with `mcp` tag.

### Testing Connection

```bash
curl -X POST http://localhost:9005/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'
```

## API Reference

### JSON-RPC Methods

| Method | Description |
|--------|-------------|
| `tools/list` | List available tools |
| `tools/call` | Execute a tool |
| `resources/list` | List available resources |
| `resources/read` | Read a resource |
| `prompts/list` | List available prompts |
| `prompts/get` | Get a prompt |

## See Also

- [NVIDIA NIM](nim.md) - AI inference on stored objects
- [Apache Iceberg](iceberg.md) - Query structured data
- [S3 Express One Zone](s3-express.md) - Low-latency storage
