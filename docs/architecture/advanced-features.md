# NebulaIO Architecture

This document provides architectural diagrams for NebulaIO's advanced features.

## System Overview

```mermaid

graph TB
    subgraph "Client Layer"
        CLI[CLI Tools]
        SDK[AWS SDKs]
        Apps[Applications]
    end

    subgraph "NebulaIO Gateway"
        LB[Load Balancer]

        subgraph "Security Layer"
            FW[Data Firewall]
            RL[Rate Limiter]
            BW[Bandwidth Controller]
        end

        subgraph "API Layer"
            S3API[S3 API Handler]
            AdminAPI[Admin API]
            ConsoleAPI[Console API]
        end

        subgraph "Feature Layer"
            Cache[DRAM Cache]
            Select[S3 Select]
            Batch[Batch Replication]
        end

        subgraph "Audit Layer"
            Audit[Audit Logger]
            Integrity[Integrity Chain]
        end
    end

    subgraph "Storage Layer"
        Meta[Metadata Store]
        Obj[Object Storage]
        Idx[Search Index]
    end

    CLI --> LB
    SDK --> LB
    Apps --> LB

    LB --> FW
    FW --> RL
    RL --> BW
    BW --> S3API
    BW --> AdminAPI
    BW --> ConsoleAPI

    S3API --> Cache
    S3API --> Select
    AdminAPI --> Batch

    Cache --> Obj
    Select --> Obj
    Batch --> Meta
    Batch --> Obj

    S3API --> Audit
    AdminAPI --> Audit
    Audit --> Integrity

```bash

## DRAM Cache Architecture

```mermaid

graph TB
    subgraph "Request Flow"
        Client[S3 Client]
        API[S3 API]
    end

    subgraph "Cache Layer"
        Lookup{Cache Lookup}

        subgraph "ARC Algorithm"
            T1[T1: Recent]
            T2[T2: Frequent]
            B1[B1: Ghost Recent]
            B2[B2: Ghost Frequent]
        end

        Prefetch[Prefetcher]
        Admit[Admission Controller]
    end

    subgraph "Storage"
        Backend[Object Storage]
    end

    Client -->|GET| API
    API --> Lookup

    Lookup -->|Hit| T1
    Lookup -->|Hit| T2
    Lookup -->|Miss| Backend

    Backend --> Admit
    Admit --> T1

    T1 -->|Promote| T2
    T1 -->|Evict| B1
    T2 -->|Evict| B2

    Prefetch -.->|Sequential Access| Backend
    Prefetch -.->|Preload| T1

```bash

### Cache Data Flow

```mermaid

sequenceDiagram
    participant C as Client
    participant A as S3 API
    participant L as Cache Lookup
    participant P as Prefetcher
    participant S as Storage

    C->>A: GetObject(key)
    A->>L: lookup(key)

    alt Cache Hit
        L-->>A: cached data
        A-->>C: 200 OK (data)
        L->>P: recordAccess(key)
        P->>P: detectPattern()
        opt Sequential Pattern
            P->>S: prefetch(next keys)
            S-->>L: store in cache
        end
    else Cache Miss
        L->>S: getObject(key)
        S-->>L: data
        L->>L: admissionCheck()
        opt Admitted
            L->>L: store(key, data)
        end
        L-->>A: data
        A-->>C: 200 OK (data)
    end

```bash

## Data Firewall Architecture

```mermaid

graph TB
    subgraph "Incoming Request"
        Req[HTTP Request]
    end

    subgraph "Firewall Pipeline"
        IP[IP Filter]
        Conn[Connection Limiter]
        Rate[Rate Limiter]
        BW[Bandwidth Tracker]
        Rules[Rule Engine]
    end

    subgraph "Limiters"
        subgraph "Token Bucket"
            TB[Tokens]
            Refill[Refill Timer]
        end

        subgraph "Sliding Window"
            SW[Window Counters]
            Slide[Window Slider]
        end
    end

    subgraph "Decision"
        Allow{Decision}
        Pass[Pass Request]
        Block[Block Request]
    end

    Req --> IP
    IP --> Conn
    Conn --> Rate
    Rate --> BW
    BW --> Rules
    Rules --> Allow

    Rate --> TB
    BW --> SW

    Allow -->|Allowed| Pass
    Allow -->|Denied| Block

```bash

### Rate Limiting Flow

```mermaid

sequenceDiagram
    participant C as Client
    participant RL as Rate Limiter
    participant TB as Token Bucket
    participant BW as Bandwidth Tracker
    participant S as S3 API

    C->>RL: Request
    RL->>TB: tryAcquire(1)

    alt Tokens Available
        TB-->>RL: acquired
        RL->>BW: trackRequest(size)

        alt Under Bandwidth Limit
            BW-->>RL: allowed
            RL->>S: forward request
            S-->>C: 200 OK
            BW->>BW: updateUsage(bytes)
        else Over Bandwidth Limit
            BW-->>RL: throttled
            RL-->>C: 503 SlowDown
        end
    else No Tokens
        TB-->>RL: rate limited
        RL-->>C: 429 TooManyRequests
    end

```bash

## S3 Select Architecture

```mermaid

graph TB
    subgraph "Request"
        Client[Client]
        API[S3 API]
    end

    subgraph "Query Engine"
        Parser[SQL Parser]
        Planner[Query Planner]
        Exec[Executor]
    end

    subgraph "Format Handlers"
        CSV[CSV Parser]
        JSON[JSON Parser]
        Parquet[Parquet Reader]
    end

    subgraph "Optimizations"
        ColPrune[Column Pruning]
        Predicate[Predicate Pushdown]
        RowGroup[Row Group Skip]
    end

    subgraph "Storage"
        Obj[Object Storage]
        Cache[DRAM Cache]
    end

    Client -->|SelectObjectContent| API
    API --> Parser
    Parser --> Planner

    Planner --> ColPrune
    Planner --> Predicate
    Planner --> RowGroup

    Planner --> Exec

    Exec --> CSV
    Exec --> JSON
    Exec --> Parquet

    CSV --> Obj
    JSON --> Obj
    Parquet --> Obj

    Obj --> Cache

```bash

### S3 Select Query Flow

```mermaid

sequenceDiagram
    participant C as Client
    participant A as S3 API
    participant P as Parser
    participant E as Executor
    participant F as Format Handler
    participant S as Storage

    C->>A: SelectObjectContent(SQL)
    A->>P: parse(SQL)
    P-->>A: AST

    A->>E: execute(AST, object)
    E->>S: streamObject()

    loop For Each Chunk
        S-->>F: chunk data
        F->>F: parse records
        F->>E: filtered records
        E->>E: apply projections
        E-->>A: result records
        A-->>C: stream results
    end

    E-->>A: stats
    A-->>C: end of results

```bash

## Batch Replication Architecture

```mermaid

graph TB
    subgraph "Job Management"
        API[Admin API]
        Queue[Job Queue]
        Scheduler[Scheduler]
    end

    subgraph "Worker Pool"
        W1[Worker 1]
        W2[Worker 2]
        W3[Worker N]
    end

    subgraph "Progress Tracking"
        Progress[Progress Tracker]
        Checkpoint[Checkpointer]
        Retry[Retry Manager]
    end

    subgraph "Source"
        SrcBucket[Source Bucket]
        SrcMeta[Source Metadata]
    end

    subgraph "Destination"
        DstEndpoint[Destination Endpoint]
        DstBucket[Destination Bucket]
    end

    API -->|Create/Start| Queue
    Queue --> Scheduler
    Scheduler --> W1
    Scheduler --> W2
    Scheduler --> W3

    W1 --> SrcBucket
    W2 --> SrcBucket
    W3 --> SrcBucket

    W1 --> DstEndpoint
    W2 --> DstEndpoint
    W3 --> DstEndpoint

    W1 --> Progress
    W2 --> Progress
    W3 --> Progress

    Progress --> Checkpoint
    Progress --> Retry

    Retry --> Queue

```bash

### Batch Job Lifecycle

```mermaid

stateDiagram-v2
    [*] --> Pending: Create Job
    Pending --> Running: Start
    Running --> Paused: Pause
    Paused --> Running: Resume
    Running --> Completed: All Done
    Running --> Failed: Error
    Running --> Cancelled: Cancel
    Paused --> Cancelled: Cancel

    Completed --> [*]
    Failed --> [*]
    Cancelled --> [*]

```bash

## Enhanced Audit Logging Architecture

```mermaid

graph TB
    subgraph "Event Sources"
        S3[S3 API]
        Admin[Admin API]
        Auth[Auth Service]
    end

    subgraph "Audit Pipeline"
        Emitter[Event Emitter]
        Buffer[Ring Buffer]
        Enricher[Enricher]
        Masker[Sensitive Data Masker]
    end

    subgraph "Integrity"
        Chain[Integrity Chain]
        HMAC[HMAC-SHA256]
        Verify[Verifier]
    end

    subgraph "Outputs"
        File[Log File]
        Webhook[Webhook]
        Export[Export API]
    end

    subgraph "Rotation"
        Rotator[Log Rotator]
        Compress[Compressor]
        Archive[Archive]
    end

    S3 --> Emitter
    Admin --> Emitter
    Auth --> Emitter

    Emitter --> Buffer
    Buffer --> Enricher
    Enricher --> Masker
    Masker --> Chain

    Chain --> HMAC
    HMAC --> File
    HMAC --> Webhook

    File --> Rotator
    Rotator --> Compress
    Compress --> Archive

    Chain --> Verify
    Export --> Verify

```bash

### Integrity Chain

```mermaid

sequenceDiagram
    participant E as Event
    participant C as Chain
    participant H as HMAC
    participant S as Storage

    E->>C: newEvent(data)
    C->>C: getPreviousHash()
    C->>H: compute(data + prevHash)
    H-->>C: currentHash
    C->>C: setEventHash(currentHash)
    C->>S: store(event)

    Note over C: Each event's hash includes<br/>the previous event's hash,<br/>creating an unbreakable chain

```bash

## Compliance Mode Configuration

```mermaid

graph LR
    subgraph "Compliance Modes"
        None[None]
        SOC2[SOC2]
        PCI[PCI-DSS]
        HIPAA[HIPAA]
        GDPR[GDPR]
        FedRAMP[FedRAMP]
    end

    subgraph "Features Enabled"
        Integrity[Integrity Chain]
        Masking[Data Masking]
        Retention[Retention Policy]
        Export[Export Formats]
        Alerts[Alert Rules]
    end

    SOC2 --> Integrity
    SOC2 --> Masking
    SOC2 --> Retention

    PCI --> Integrity
    PCI --> Masking
    PCI --> Alerts

    HIPAA --> Integrity
    HIPAA --> Masking
    HIPAA --> Retention
    HIPAA --> Export

    GDPR --> Masking
    GDPR --> Retention
    GDPR --> Export

    FedRAMP --> Integrity
    FedRAMP --> Masking
    FedRAMP --> Retention
    FedRAMP --> Alerts

```bash

## Complete Feature Integration

```mermaid

graph TB
    subgraph "Request Path"
        Client[Client Request]

        subgraph "Security"
            Firewall[Data Firewall]
            RateLimit[Rate Limiter]
            Bandwidth[Bandwidth Control]
        end

        subgraph "Processing"
            API[S3 API]
            Select[S3 Select]
            Cache[DRAM Cache]
        end

        subgraph "Storage"
            Objects[Object Store]
            Metadata[Metadata]
        end

        subgraph "Background"
            Batch[Batch Replication]
            Audit[Audit Logger]
        end
    end

    Client --> Firewall
    Firewall --> RateLimit
    RateLimit --> Bandwidth
    Bandwidth --> API

    API --> Cache
    API --> Select
    Cache --> Objects
    Select --> Objects

    API --> Audit

    Objects --> Batch
    Metadata --> Batch

    Batch -->|Replicate| External[Remote Cluster]
    Audit -->|Export| SIEM[SIEM System]

```

## Performance Characteristics

| Component | Metric | Value |
| ----------- | -------- | ------- |
| DRAM Cache | Read Latency (p50) | < 50μs |
| DRAM Cache | Read Latency (p99) | < 150μs |
| DRAM Cache | Throughput | 10+ GB/s |
| Rate Limiter | Decision Time | < 1μs |
| Bandwidth Tracker | Overhead | < 100ns/request |
| S3 Select | Query Speedup | 10-100x |
| Audit Logger | Events/sec | 100,000+ |
| Batch Replication | Throughput | 1+ GB/s |
