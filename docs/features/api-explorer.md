# API Explorer

NebulaIO's API Explorer provides interactive documentation for the Admin and S3 APIs with Swagger UI integration, code snippet generation, and request history tracking.

## Overview

The API Explorer delivers:

- **Interactive Documentation**: Full OpenAPI/Swagger UI interface for exploring APIs
- **Code Snippet Generation**: Automatic code generation for cURL, JavaScript, Python, and Go
- **Request History**: Track and replay previous API requests
- **Authentication Integration**: Automatic JWT token injection for authenticated requests
- **Export Capabilities**: Download OpenAPI specification in JSON or YAML format

## Architecture

```text

┌─────────────────────────────────────────────────────────────┐
│                     API Explorer                             │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  Swagger UI │  │   Request   │  │   Code Snippet      │  │
│  │  Component  │──│   History   │──│   Generator         │  │
│  │             │  │  (Storage)  │  │   (Multi-Language)  │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│         │                                    │               │
│         ▼                                    ▼               │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                  OpenAPI Handler                         ││
│  └────────────┬───────────────┬────────────────┬───────────┘│
│               │               │                │             │
│               ▼               ▼                ▼             │
│        ┌──────────┐   ┌──────────────┐  ┌──────────────┐    │
│        │   JSON   │   │    YAML      │  │    Config    │    │
│        │   Spec   │   │    Spec      │  │   Endpoint   │    │
│        └──────────┘   └──────────────┘  └──────────────┘    │
└─────────────────────────────────────────────────────────────┘

```

## Accessing the API Explorer

The API Explorer is available in the NebulaIO web console:

1. Navigate to **Admin Console** (default: `https://localhost:9001`)
2. Log in with your admin credentials
3. Click **API Explorer** in the sidebar navigation

## Features

### Interactive API Documentation

The Swagger UI interface provides:

- **Endpoint Browsing**: Explore all available API endpoints organized by tag
- **Try It Out**: Execute API requests directly from the browser
- **Schema Visualization**: View request/response schemas with examples
- **Authentication**: Automatic JWT token injection for authenticated endpoints

### Code Snippet Generation

Generate ready-to-use code snippets in multiple languages:

| Language   | Format              | Use Case                    |
|------------|--------------------|-----------------------------|
| cURL       | Shell command      | Quick testing, scripts      |
| JavaScript | Fetch API          | Frontend/Node.js apps       |
| Python     | Requests library   | Backend scripts, automation |
| Go         | net/http           | Go services                 |

### Request History

Track your API exploration:

- **Automatic Logging**: All requests are automatically recorded
- **Persistent Storage**: History saved in browser localStorage
- **Search & Filter**: Find previous requests by path or method
- **Replay**: Re-execute previous requests with one click
- **Export**: Generate code snippets from historical requests

## API Endpoints

### OpenAPI Specification

| Endpoint                       | Method | Description                        |
|-------------------------------|--------|------------------------------------|
| `/api/v1/openapi.json`        | GET    | OpenAPI spec in JSON format        |
| `/api/v1/openapi.yaml`        | GET    | OpenAPI spec in YAML format        |
| `/api/v1/openapi`             | GET    | OpenAPI spec (format via Accept)   |

### API Explorer Config

| Endpoint                       | Method | Description                        |
|-------------------------------|--------|------------------------------------|
| `/api/v1/api-explorer/config` | GET    | API Explorer configuration         |

### Code Snippet Generation

| Endpoint                              | Method | Description                        |
|--------------------------------------|--------|------------------------------------|
| `/api/v1/api-explorer/code-snippets` | POST   | Generate code snippets             |

**Request Body:**

```json
{
  "method": "GET",
  "url": "https://localhost:9001/api/v1/buckets",
  "headers": {
    "Authorization": "Bearer <token>",
    "Content-Type": "application/json"
  },
  "body": ""
}
```

**Response:**

```json
{
  "snippets": [
    {
      "language": "curl",
      "code": "curl -X GET \\\n  -H 'Authorization: Bearer <token>' \\\n  'https://localhost:9001/api/v1/buckets'"
    },
    {
      "language": "javascript",
      "code": "const response = await fetch('https://localhost:9001/api/v1/buckets', {...});"
    }
  ]
}
```

## Security

The API Explorer implements comprehensive security measures:

### Input Validation

- **URL Validation**: Only HTTP/HTTPS URLs allowed, max 2048 characters
- **Method Whitelist**: GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS
- **Header Limits**: Maximum 50 headers, key length 256, value length 8192
- **Body Size Limit**: Maximum 1MB request body

### Code Injection Prevention

All user input is properly escaped in generated code snippets:

- **Shell**: Single quotes escaped for cURL commands
- **JavaScript**: String escape sequences for special characters
- **Python**: String escape sequences for special characters
- **Go**: Both raw strings and escaped strings supported

### Header Sanitization

Header keys are sanitized to only allow alphanumeric characters, hyphens, and underscores.

## Configuration

The API Explorer is enabled by default. Configuration options:

```yaml
api_explorer:
  enabled: true
  title: "NebulaIO API Explorer"
  description: "Interactive API documentation for NebulaIO Admin and S3 APIs"
  version: "1.0.0"
  base_url: "/api/v1"
  supported_languages:
    - curl
    - javascript
    - python
    - go
  max_request_history_size: 50
```

## Exporting the OpenAPI Spec

Download the OpenAPI specification for use with other tools:

### From the UI

1. Click **Export JSON** or **Export YAML** in the API Explorer header
2. The specification file will be downloaded to your browser

### Via API

```bash
# JSON format
curl -o openapi.json https://localhost:9001/api/v1/openapi.json

# YAML format
curl -o openapi.yaml https://localhost:9001/api/v1/openapi.yaml
```

## Troubleshooting

### Authentication Issues

If you see "No authentication token" warning:

1. Ensure you are logged into the admin console
2. Check that your session hasn't expired
3. Log out and log back in to refresh your JWT token

### Request History Not Saving

Request history is stored in browser localStorage:

1. Ensure localStorage is enabled in your browser
2. Clear browser cache if history appears corrupted
3. Check available storage space

### Swagger UI Not Loading

If the Swagger UI fails to load:

1. Check browser console for JavaScript errors
2. Verify the OpenAPI spec endpoint is accessible
3. Ensure no browser extensions are blocking the content
