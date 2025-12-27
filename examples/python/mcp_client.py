#!/usr/bin/env python3
"""
MCP Server Client Example

Demonstrates how to interact with NebulaIO's MCP Server for AI agent integration.
"""

import os
import json
import requests
from typing import Any

# Configuration
MCP_ENDPOINT = os.getenv("NEBULAIO_MCP_ENDPOINT", "http://localhost:9005/mcp")
ACCESS_KEY = os.getenv("NEBULAIO_ACCESS_KEY", "minioadmin")
SECRET_KEY = os.getenv("NEBULAIO_SECRET_KEY", "minioadmin")


class MCPClient:
    """Simple MCP client for NebulaIO."""

    def __init__(self, endpoint: str, access_key: str, secret_key: str):
        self.endpoint = endpoint
        self.auth = f"{access_key}:{secret_key}"
        self.request_id = 0

    def _send_request(self, method: str, params: dict = None) -> dict:
        """Send a JSON-RPC request to the MCP server."""
        self.request_id += 1
        payload = {
            "jsonrpc": "2.0",
            "id": self.request_id,
            "method": method,
            "params": params or {},
        }

        response = requests.post(
            self.endpoint,
            json=payload,
            headers={
                "Content-Type": "application/json",
                "Authorization": f"Bearer {self.auth}",
            },
        )
        response.raise_for_status()
        result = response.json()

        if "error" in result:
            raise Exception(f"MCP Error: {result['error']}")

        return result.get("result", {})

    def list_tools(self) -> list:
        """List available MCP tools."""
        result = self._send_request("tools/list")
        return result.get("tools", [])

    def call_tool(self, name: str, arguments: dict = None) -> Any:
        """Call an MCP tool."""
        result = self._send_request(
            "tools/call", {"name": name, "arguments": arguments or {}}
        )
        return result

    def list_resources(self) -> list:
        """List available resources."""
        result = self._send_request("resources/list")
        return result.get("resources", [])

    def read_resource(self, uri: str) -> str:
        """Read a resource by URI."""
        result = self._send_request("resources/read", {"uri": uri})
        contents = result.get("contents", [])
        if contents:
            return contents[0].get("text", "")
        return ""


def list_buckets(client: MCPClient):
    """List all buckets using MCP."""
    print("\n1. Listing buckets:")
    result = client.call_tool("list_buckets")
    print(f"   Result: {result}")


def list_objects(client: MCPClient, bucket: str, prefix: str = ""):
    """List objects in a bucket."""
    print(f"\n2. Listing objects in '{bucket}':")
    result = client.call_tool(
        "list_objects", {"bucket": bucket, "prefix": prefix, "max_keys": 10}
    )
    print(f"   Result: {result}")


def read_object(client: MCPClient, bucket: str, key: str):
    """Read an object's content."""
    print(f"\n3. Reading object '{key}':")
    result = client.call_tool("read_object", {"bucket": bucket, "key": key})
    content = result.get("content", [{}])[0].get("text", "")
    print(f"   Content: {content[:200]}...")  # First 200 chars


def write_object(client: MCPClient, bucket: str, key: str, content: str):
    """Write content to an object."""
    print(f"\n4. Writing object '{key}':")
    result = client.call_tool(
        "write_object",
        {"bucket": bucket, "key": key, "content": content, "content_type": "text/plain"},
    )
    print(f"   Result: {result}")


def browse_resources(client: MCPClient):
    """Browse available resources."""
    print("\n5. Browsing resources:")
    resources = client.list_resources()
    for resource in resources[:5]:  # First 5
        print(f"   - {resource.get('name')}: {resource.get('uri')}")


def read_resource_content(client: MCPClient, bucket: str, key: str):
    """Read content using resource URI."""
    print(f"\n6. Reading resource s3://{bucket}/{key}:")
    content = client.read_resource(f"s3://{bucket}/{key}")
    print(f"   Content: {content[:200]}...")


def main():
    print("MCP Server Client Example")
    print("=" * 50)

    # Create client
    client = MCPClient(MCP_ENDPOINT, ACCESS_KEY, SECRET_KEY)

    # List available tools
    print("\nAvailable MCP tools:")
    tools = client.list_tools()
    for tool in tools:
        print(f"  - {tool['name']}: {tool.get('description', 'No description')}")

    # Demo operations
    list_buckets(client)

    # Create test bucket and object (using S3 client first)
    import boto3

    s3 = boto3.client(
        "s3",
        endpoint_url=os.getenv("NEBULAIO_ENDPOINT", "http://localhost:9000"),
        aws_access_key_id=ACCESS_KEY,
        aws_secret_access_key=SECRET_KEY,
    )

    bucket = "mcp-demo"
    try:
        s3.create_bucket(Bucket=bucket)
    except:
        pass

    s3.put_object(
        Bucket=bucket,
        Key="test/sample.txt",
        Body="Hello from NebulaIO MCP!\nThis is a test document for AI agents.",
    )

    # Use MCP to interact with the data
    list_objects(client, bucket, "test/")
    read_object(client, bucket, "test/sample.txt")
    write_object(client, bucket, "test/output.txt", "Written via MCP!")

    browse_resources(client)
    read_resource_content(client, bucket, "test/sample.txt")

    print("\nDone!")


if __name__ == "__main__":
    main()
