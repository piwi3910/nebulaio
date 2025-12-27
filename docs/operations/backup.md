# Backup and Recovery

NebulaIO provides comprehensive backup and recovery capabilities to protect data against accidental deletion, corruption, or disaster scenarios.

## Backup Strategies Overview

| Strategy | RPO | RTO | Use Case |
|----------|-----|-----|----------|
| Continuous Replication | Near-zero | Minutes | Mission-critical data |
| Point-in-Time Recovery | Configurable | Hours | Compliance, audit trails |
| Scheduled Snapshots | Hours | Hours | General production |
| Cross-Site Backup | Hours | Hours-Days | Disaster recovery |

---

## Data Backup Methods

### Full Bucket Backup

```bash
# Backup bucket to another NebulaIO cluster
nebulaio-cli admin backup create \
  --source-bucket production-data \
  --destination s3://backup-cluster/backups/production-data \
  --destination-endpoint https://backup.example.com:9000

# Backup to local filesystem
nebulaio-cli admin backup create \
  --source-bucket production-data \
  --destination file:///mnt/backup/production-data-$(date +%Y%m%d)
```

### Incremental Backup

```bash
nebulaio-cli admin backup create \
  --source-bucket production-data \
  --destination s3://backup-cluster/backups/production-data \
  --incremental \
  --since-marker /var/lib/nebulaio/backup-markers/production-data.marker
```

---

## Metadata Backup

Export cluster configuration, policies, and user definitions.

```bash
nebulaio-cli admin metadata export \
  --output /backup/metadata/cluster-metadata-$(date +%Y%m%d).json

nebulaio-cli admin metadata export \
  --include users,policies,buckets,replication-rules \
  --output /backup/metadata/config-backup.json
```

### Automated Metadata Backup

```yaml
metadata_backup:
  enabled: true
  schedule: "0 2 * * *"
  destination: s3://backup-bucket/metadata/
  retention_days: 90
  include: [users, groups, policies, bucket_configurations]
```

---

## Point-in-Time Recovery (PITR)

PITR enables recovery to any point within the configured retention window.

### Enable PITR

```yaml
pitr:
  enabled: true
  retention_period: 7d
  checkpoint_interval: 5m
  storage:
    type: s3
    bucket: pitr-data
```

### Recover to Point in Time

```bash
nebulaio-cli admin pitr list-points --bucket critical-data

nebulaio-cli admin pitr recover \
  --bucket critical-data \
  --target-bucket critical-data-recovered \
  --point-in-time "2024-01-15T14:30:00Z"
```

---

## Cross-Site Backup

### Configure Remote Backup Target

```yaml
backup:
  remote_targets:
    - name: dr-site
      endpoint: https://dr.example.com:9000
      access_key: ${DR_ACCESS_KEY}
      secret_key: ${DR_SECRET_KEY}
      bucket: disaster-recovery
      encryption:
        enabled: true
        key_id: backup-encryption-key
```

### Scheduled Cross-Site Backup

```yaml
backup:
  schedules:
    - name: daily-dr-backup
      source_buckets: ["production-*", "critical-*"]
      target: dr-site
      schedule: "0 3 * * *"
      retention_count: 30
      type: incremental
```

---

## Restore Procedures

### Full Bucket Restore

```bash
nebulaio-cli admin restore \
  --source s3://backup-cluster/backups/production-data/20240115 \
  --source-endpoint https://backup.example.com:9000 \
  --target-bucket production-data-restored
```

### Selective Restore

```bash
nebulaio-cli admin restore \
  --source s3://backup-cluster/backups/production-data/20240115 \
  --target-bucket production-data \
  --prefix "reports/2024/"
```

### Metadata Restore

```bash
nebulaio-cli admin metadata import \
  --input /backup/metadata/cluster-metadata-20240115.json

nebulaio-cli admin metadata import \
  --input /backup/metadata/cluster-metadata-20240115.json \
  --dry-run
```

---

## Backup Verification

### Automated Verification

```yaml
backup:
  verification:
    enabled: true
    schedule: "0 6 * * *"
    sample_percentage: 10
    checksum_validation: true
    report_destination: s3://backup-bucket/verification-reports/
```

### Manual Verification

```bash
nebulaio-cli admin backup verify \
  --backup-path s3://backup-cluster/backups/production-data/20240115 \
  --source-bucket production-data \
  --checksum
```

---

## Scheduling and Automation

### Backup Schedule Configuration

```yaml
backup:
  global_settings:
    default_retention_days: 30
    compression: zstd
    encryption:
      enabled: true
      key_id: backup-master-key

  schedules:
    - name: hourly-critical
      buckets: ["critical-*"]
      schedule: "0 * * * *"
      type: incremental
      retention_count: 24

    - name: daily-production
      buckets: ["production-*"]
      schedule: "0 2 * * *"
      type: incremental
      retention_count: 30

    - name: weekly-full
      buckets: ["*"]
      schedule: "0 3 * * 0"
      type: full
      retention_count: 12
```

### Monitoring Backup Jobs

```bash
nebulaio-cli admin backup list-jobs
nebulaio-cli admin backup job-status --job-id backup-123
nebulaio-cli admin backup history --bucket production-data --limit 10
```

### Prometheus Metrics

```
nebulaio_backup_jobs_total{status="success|failed|running"}
nebulaio_backup_duration_seconds{bucket="...",type="full|incremental"}
nebulaio_backup_last_success_timestamp{bucket="..."}
nebulaio_pitr_checkpoint_lag_seconds{bucket="..."}
```

---

## Best Practices

1. **Follow 3-2-1 Rule**: 3 copies, 2 media types, 1 offsite
2. **Test Restores Regularly**: Schedule monthly restore drills
3. **Encrypt All Backups**: Use encryption at rest and in transit
4. **Monitor Backup Health**: Set alerts for failed or stale backups
5. **Document Procedures**: Maintain runbooks for recovery scenarios
6. **Validate Checksums**: Verify backup integrity after creation

## Next Steps

- [Site Replication](../features/site-replication.md) - Active-active replication
- [Replication](../features/replication.md) - Bucket replication rules
- [Object Lock](../features/object-lock.md) - Immutable backups
