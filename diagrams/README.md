# NebulaIO Architecture Diagrams

This directory contains architecture diagrams for NebulaIO in Mermaid format.

## Diagrams

### Security Architecture
[security-architecture.mmd](security-architecture.mmd)

Shows the complete security layer including:
- Access Analytics with anomaly detection
- Encryption key management and rotation
- mTLS internal communication
- OpenTelemetry distributed tracing
- Secret scanning and DLP
- Integration with external systems (SIEM, Prometheus, Jaeger)

## Viewing Diagrams

### VS Code
Install the "Mermaid Preview" extension and open any `.mmd` file.

### GitHub
GitHub renders Mermaid diagrams automatically in markdown files.

### Online Editor
Copy diagram content to [Mermaid Live Editor](https://mermaid.live/)

### Command Line
```bash
# Install mermaid-cli
npm install -g @mermaid-js/mermaid-cli

# Generate PNG
mmdc -i security-architecture.mmd -o security-architecture.png

# Generate SVG
mmdc -i security-architecture.mmd -o security-architecture.svg

# Generate PDF
mmdc -i security-architecture.mmd -o security-architecture.pdf
```

## Architecture Overview

```
                    NebulaIO Architecture
    ┌─────────────────────────────────────────────────────┐
    │                  External Access                     │
    │    S3 Clients | Admin API | AI Agents | Web Console │
    └─────────────────────────┬───────────────────────────┘
                              │
    ┌─────────────────────────▼───────────────────────────┐
    │               Gateway Layer (TLS/mTLS)              │
    │  Authentication | Rate Limiting | Data Firewall    │
    └─────────────────────────┬───────────────────────────┘
                              │
    ┌─────────────────────────▼───────────────────────────┐
    │                  Security Layer                      │
    │  ┌──────────────┐  ┌────────────┐  ┌─────────────┐ │
    │  │   Access     │  │   Key      │  │   Content   │ │
    │  │  Analytics   │  │  Rotation  │  │  Scanning   │ │
    │  └──────────────┘  └────────────┘  └─────────────┘ │
    └─────────────────────────┬───────────────────────────┘
                              │
    ┌─────────────────────────▼───────────────────────────┐
    │                 Observability Layer                  │
    │  OpenTelemetry | Prometheus Metrics | Audit Logs   │
    └─────────────────────────┬───────────────────────────┘
                              │
    ┌─────────────────────────▼───────────────────────────┐
    │                    S3 API Layer                      │
    │   Core Operations | Express | Iceberg | MCP        │
    └─────────────────────────┬───────────────────────────┘
                              │
    ┌─────────────────────────▼───────────────────────────┐
    │                   Storage Layer                      │
    │  Encrypted Objects | Metadata | Cache | Tiering    │
    └─────────────────────────────────────────────────────┘
```
