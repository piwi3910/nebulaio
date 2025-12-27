# Upgrading NebulaIO

This guide covers upgrading NebulaIO to newer versions across all deployment types.

## Version Compatibility

NebulaIO follows semantic versioning (MAJOR.MINOR.PATCH):

| Version Change | Compatibility | Data Migration |
|----------------|---------------|----------------|
| Patch (x.x.1 to x.x.2) | Fully compatible | None required |
| Minor (x.1.x to x.2.x) | Backward compatible | Automatic |
| Major (1.x.x to 2.x.x) | May break compatibility | Manual required |

All nodes in a cluster must run the same minor version. Mixed versions are only supported during rolling upgrades.

---

## Pre-Upgrade Checklist

### 1. Review Release Notes

```bash
nebulaio --version
curl -s https://api.github.com/repos/piwi3910/nebulaio/releases/tags/v1.3.0 | jq '.body'
```

### 2. Backup Current State

```bash
cp -r /etc/nebulaio /etc/nebulaio.backup.$(date +%Y%m%d)
nebulaio admin backup create --output /backup/pre-upgrade-$(date +%Y%m%d).tar.gz
```

### 3. Verify Cluster Health

```bash
curl -s http://localhost:9001/api/v1/admin/cluster/health | jq
curl -s http://localhost:9001/api/v1/admin/cluster/nodes | jq '.nodes[] | {name, status}'
```

---

## Upgrade Procedures

### Standalone Upgrade {#standalone}

```bash
sudo systemctl stop nebulaio
sudo cp /usr/local/bin/nebulaio /usr/local/bin/nebulaio.backup

curl -LO https://github.com/piwi3910/nebulaio/releases/download/v1.3.0/nebulaio-linux-amd64.tar.gz
tar -xzf nebulaio-linux-amd64.tar.gz
sudo mv nebulaio /usr/local/bin/

sudo systemctl start nebulaio
curl -s http://localhost:9001/health/ready
```

### Docker Upgrade {#docker}

```bash
docker pull ghcr.io/piwi3910/nebulaio:v1.3.0
docker stop nebulaio && docker rm nebulaio

docker run -d --name nebulaio \
  -p 9000:9000 -p 9001:9001 -p 9002:9002 \
  -v nebulaio-data:/data \
  ghcr.io/piwi3910/nebulaio:v1.3.0
```

For Docker Compose, update the image tag and run:

```bash
docker-compose pull && docker-compose up -d
```

### Kubernetes Upgrade {#kubernetes}

```bash
kubectl set image statefulset/nebulaio \
  nebulaio=ghcr.io/piwi3910/nebulaio:v1.3.0 -n nebulaio
kubectl rollout status statefulset/nebulaio -n nebulaio --timeout=10m
```

---

## Rolling Upgrades {#rolling-upgrades}

Rolling upgrades minimize downtime by upgrading nodes one at a time.

### Prerequisites

- Minimum 3-node cluster
- All nodes healthy before starting

### Procedure

```bash
# Identify leader (upgrade last)
LEADER=$(curl -s http://localhost:9001/api/v1/admin/cluster/leader | jq -r '.node_id')

# Upgrade non-leader nodes first
for NODE in node-2 node-3; do
  curl -X POST "http://$NODE:9001/api/v1/admin/node/drain"
  sleep 30
  # Perform upgrade on $NODE
  until curl -s "http://$NODE:9001/health/ready" | grep -q "ok"; do sleep 5; done
done

# Upgrade leader last
curl -X POST "http://$LEADER:9001/api/v1/admin/node/drain"
# Perform upgrade on leader
```

Kubernetes StatefulSets handle rolling updates automatically with `updateStrategy: RollingUpdate`.

---

## Rollback Procedures {#rollback}

### Standalone

```bash
sudo systemctl stop nebulaio
sudo mv /usr/local/bin/nebulaio.backup /usr/local/bin/nebulaio
sudo systemctl start nebulaio
```

### Docker

```bash
docker stop nebulaio && docker rm nebulaio
docker run -d --name nebulaio \
  -p 9000:9000 -p 9001:9001 -p 9002:9002 \
  -v nebulaio-data:/data \
  ghcr.io/piwi3910/nebulaio:v1.2.0  # Previous version
```

### Kubernetes

```bash
kubectl rollout history statefulset/nebulaio -n nebulaio
kubectl rollout undo statefulset/nebulaio -n nebulaio
```

---

## Post-Upgrade Verification {#verification}

```bash
# Version and health
curl -s http://localhost:9001/api/v1/admin/info | jq '.version'
curl -s http://localhost:9001/api/v1/admin/cluster/health | jq

# Functional test
aws s3 --endpoint-url http://localhost:9000 ls
aws s3 --endpoint-url http://localhost:9000 mb s3://upgrade-test
aws s3 --endpoint-url http://localhost:9000 cp /tmp/testfile s3://upgrade-test/
aws s3 --endpoint-url http://localhost:9000 rb s3://upgrade-test --force

# Metrics
curl -s http://localhost:9001/metrics | grep nebulaio_http_requests
```

---

## Breaking Changes Notes {#breaking-changes}

### Version 2.0.0

- **Configuration format**: YAML structure updated; run `nebulaio admin migrate-config`
- **API changes**: `/api/v1/admin/stats` replaced by `/api/v2/admin/metrics`
- **Authentication**: JWT token format changed; users must re-authenticate

### Version 1.5.0

- **Raft protocol**: All nodes must upgrade simultaneously
- **Storage format**: Automatic migration on first start

### Version 1.3.0

- **Environment variables**: `NEBULAIO_ROOT_*` renamed to `NEBULAIO_AUTH_ROOT_*`
- **Port change**: Console moved from 9001 to 9002

### Migration Tools

```bash
nebulaio admin migrate-config --from-version 1.2.0 --input /etc/nebulaio/config.yaml
nebulaio admin validate-config --config /etc/nebulaio/config.yaml.migrated
```

---

## Next Steps

- [Backup & Recovery](backup.md)
- [Monitoring Setup](monitoring.md)
- [Troubleshooting Guide](../../TROUBLESHOOTING.md)
