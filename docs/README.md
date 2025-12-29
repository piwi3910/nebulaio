# NebulaIO Documentation

Welcome to the NebulaIO documentation. This guide covers everything from getting started to production deployment and operations.

## Table of Contents

### Getting Started

- [Quick Start Guide](getting-started/quick-start.md) - Get NebulaIO running in minutes
- [Configuration Reference](getting-started/configuration.md) - All configuration options explained
- [First Steps](getting-started/first-steps.md) - Create your first bucket and upload objects
- [CLI Tool](getting-started/cli.md) - Using the nebulaio-cli command-line tool

### Deployment Guides

- [Standalone Deployment](deployment/standalone.md) - Single binary installation
- [Docker Deployment](deployment/docker.md) - Container-based deployment
- [Kubernetes Deployment](deployment/kubernetes.md) - Cloud-native deployment
- [Helm Chart](deployment/helm.md) - Kubernetes package manager deployment
- [Operator Deployment](deployment/operator.md) - Kubernetes operator for automated management

### Architecture

- [System Overview](architecture/overview.md) - High-level architecture
- [Clustering & HA](architecture/clustering.md) - Distributed deployment patterns
- [Storage Backend](architecture/storage.md) - Storage layer details
- [Security Model](architecture/security.md) - Authentication and authorization

### Advanced Features

- [Erasure Coding](features/erasure-coding.md) - Data durability with Reed-Solomon encoding
- [Data Compression](features/compression.md) - Zstandard, LZ4, and Gzip compression
- [Bucket Replication](features/replication.md) - Async bucket replication configuration
- [Site Replication](features/site-replication.md) - Multi-datacenter Active-Active sync
- [LDAP Integration](features/ldap.md) - LDAP/Active Directory authentication
- [OIDC/SSO](features/oidc.md) - OpenID Connect single sign-on
- [KMS Integration](features/kms.md) - Key Management System integration
- [Event Notifications](features/events.md) - Webhook, Kafka, and messaging integration
- [Storage Tiering](features/tiering.md) - Hot/Cold storage with caching
- [Object Lock](features/object-lock.md) - WORM compliance and legal holds

### Operations

- [Monitoring](operations/monitoring.md) - Prometheus metrics and alerting
- [Backup & Recovery](operations/backup.md) - Data protection strategies
- [Scaling](operations/scaling.md) - Horizontal and vertical scaling
- [Troubleshooting](operations/troubleshooting.md) - Common issues and solutions
- [Upgrading](operations/upgrading.md) - Version upgrade procedures

### API Reference

- [S3 API Compatibility](api/s3-compatibility.md) - Supported S3 operations
- [Admin API](api/admin-api.md) - Management API reference
- [Console API](api/console-api.md) - User-facing API reference

## Quick Links

| Deployment Type | Single Node | HA Cluster | Production |
| ---------------- | ------------- | ------------ | ------------ |
| **Standalone** | [Guide](deployment/standalone.md#single-node) | [Guide](deployment/standalone.md#ha-cluster) | [Guide](deployment/standalone.md#production) |
| **Docker** | [Guide](deployment/docker.md#single-node) | [Guide](deployment/docker.md#ha-cluster) | [Guide](deployment/docker.md#production) |
| **Kubernetes** | [Guide](deployment/kubernetes.md#single-node) | [Guide](deployment/kubernetes.md#ha-cluster) | [Guide](deployment/kubernetes.md#production) |
| **Helm** | [Guide](deployment/helm.md#single-node) | [Guide](deployment/helm.md#ha-cluster) | [Guide](deployment/helm.md#production) |

## Architecture Diagram

```text

                                    ┌─────────────────────────────────────────┐
                                    │             Load Balancer               │
                                    │        (HAProxy / Nginx / Cloud LB)     │
                                    └──────────────┬───────────┬──────────────┘
                                                   │           │
                    ┌──────────────────────────────┼───────────┼──────────────────────────────┐
                    │                              │           │                              │
           ┌────────▼────────┐            ┌────────▼────────┐            ┌────────▼────────┐
           │   NebulaIO #1   │            │   NebulaIO #2   │            │   NebulaIO #3   │
           │                 │◄──────────►│                 │◄──────────►│                 │
           │  S3 API :9000   │   Gossip   │  S3 API :9000   │   Gossip   │  S3 API :9000   │
           │  Admin  :9001   │   :9004    │  Admin  :9001   │   :9004    │  Admin  :9001   │
           │  Raft   :9003   │            │  Raft   :9003   │            │  Raft   :9003   │
           └────────┬────────┘            └────────┬────────┘            └────────┬────────┘
                    │                              │                              │
           ┌────────▼────────┐            ┌────────▼────────┐            ┌────────▼────────┐
           │  Local Storage  │            │  Local Storage  │            │  Local Storage  │
           │    /data/node1  │            │    /data/node2  │            │    /data/node3  │
           └─────────────────┘            └─────────────────┘            └─────────────────┘

```

## Getting Help

- **GitHub Issues**: [Report bugs or request features](https://github.com/piwi3910/nebulaio/issues)
- **Discussions**: [Community discussions](https://github.com/piwi3910/nebulaio/discussions)

## Contributing

We welcome contributions! See our [Contributing Guide](../CONTRIBUTING.md) for details.
