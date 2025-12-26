# NebulaIO Project Overview

## Purpose
NebulaIO is an S3-compatible object storage system written in Go. It provides a fully compatible S3 API for object storage with features like:
- S3-compatible API endpoints
- Admin API for system management
- Console API for user-facing operations
- JWT-based authentication
- AWS Signature V4 authentication for S3 API
- Presigned URL support
- Bucket and object management
- Multipart uploads
- Versioning support
- Lifecycle management

## Tech Stack
- **Language**: Go 1.25+
- **HTTP Router**: chi/v5
- **Logging**: zerolog
- **Configuration**: viper
- **JWT**: golang-jwt/jwt/v5
- **Password Hashing**: bcrypt (golang.org/x/crypto)
- **Storage**: Filesystem-based (BadgerDB for metadata)
- **Consensus**: Hashicorp Raft for distributed metadata

## Project Structure
```
nebulaio/
├── cmd/nebulaio/       # Main entry point
├── internal/
│   ├── api/            # HTTP API handlers
│   │   ├── admin/      # Admin API (user/bucket management)
│   │   ├── console/    # Console API (user-facing)
│   │   ├── middleware/ # HTTP middleware
│   │   └── s3/         # S3 API handlers
│   ├── auth/           # Authentication services
│   │   ├── service.go  # JWT and user auth
│   │   ├── password.go # Password utilities
│   │   ├── presigned.go # Presigned URL generation
│   │   └── signature.go # AWS Signature V4 validation
│   ├── bucket/         # Bucket service
│   ├── config/         # Configuration loading
│   ├── lifecycle/      # Object lifecycle management
│   ├── metadata/       # Metadata storage (Raft-backed)
│   ├── multipart/      # Multipart upload handling
│   ├── object/         # Object service
│   ├── server/         # HTTP servers setup
│   ├── storage/        # Storage backends
│   └── versioning/     # Object versioning
├── pkg/
│   └── s3types/        # S3 XML types
├── web/                # Web console frontend
├── docs/               # Documentation
├── scripts/            # Build/deployment scripts
└── deployments/        # Deployment configurations
```

## Ports
- S3 API: 9000 (default)
- Admin API: 9001 (default)
- Console (Web UI): 9002 (default)
- Raft: 9003 (default)
