# NebulaIO Troubleshooting Guide

This guide covers common issues and diagnostic approaches for NebulaIO deployments.

## Table of Contents

1. [Common Issues and Solutions](#common-issues-and-solutions)
2. [Diagnostic Commands](#diagnostic-commands)
3. [Log Analysis](#log-analysis)
4. [Network Troubleshooting](#network-troubleshooting)
5. [Storage Issues](#storage-issues)
6. [Cluster Health Problems](#cluster-health-problems)
7. [Performance Issues](#performance-issues)
8. [Getting Support](#getting-support)

---

## Common Issues and Solutions

### Service Won't Start

```bash

# Check port conflicts
lsof -i :9000 && lsof -i :9001

# Validate configuration
nebulaio config validate --config=/etc/nebulaio/config.yaml

# Check permissions and view errors
ls -la /var/lib/nebulaio/
journalctl -u nebulaio --no-pager -n 50

```bash

### Authentication Failures

```bash

# Verify user and reset credentials
nebulaio admin user info --username=myuser
nebulaio admin user accesskey create --username=myuser
nebulaio admin user unlock --username=myuser
timedatectl status  # Time must be within 15 minutes

```bash

### Upload/Download Failures

```bash

# Check disk space and verify object
df -h /var/lib/nebulaio
nebulaio admin object verify --bucket=mybucket --key=mykey
dmesg | tail -50 | grep -i error

```text

---

## Diagnostic Commands

### Health Checks

```bash

nebulaio diag --all                                              # Full diagnostics
curl -sf http://localhost:9000/minio/health/live && echo "OK"    # Liveness
curl -sf http://localhost:9000/minio/health/ready && echo "OK"   # Readiness
nebulaio cluster status                                          # Cluster state

```bash

### Resource Monitoring

```bash

ps aux | grep nebulaio                     # Process stats
ls /proc/$(pgrep nebulaio)/fd | wc -l      # Open file descriptors
ss -tlnp | grep nebulaio                   # Network connections

```text

---

## Log Analysis

### Log Locations

| Component | Location |
| ----------- | ---------- |
| Server | `/var/log/nebulaio/server.log` |
| API | `/var/log/nebulaio/api.log` |
| Cluster | `/var/log/nebulaio/cluster.log` |
| Audit | `/var/log/nebulaio/audit.log` |

### Useful Queries

```bash

# Recent errors
journalctl -u nebulaio --since "1 hour ago" | grep -i error

# Slow requests (>1 second)
grep "duration" /var/log/nebulaio/api.log | awk -F'duration=' '{print $2}' | awk '$1 > 1000'

# Authentication failures and cluster events
grep "auth" /var/log/nebulaio/audit.log | grep -i fail
grep -E "(join|leave|leader)" /var/log/nebulaio/cluster.log

```bash

### Enable Debug Logging

```yaml

# config.yaml
logging:
  level: debug

```text

```bash

nebulaio admin log level set debug  # Runtime toggle

```text

---

## Network Troubleshooting

### Connectivity Tests

```bash

nc -zv localhost 9000                               # S3 API port
nc -zv localhost 9001                               # Console port
nc -zv peer-node 9003                               # Cluster port
iptables -L -n | grep -E "(9000|9001|9003)"         # Firewall rules

```bash

### TLS Issues

```bash

openssl x509 -in /etc/nebulaio/certs/server.crt -noout -dates           # Check validity
openssl verify -CAfile /etc/nebulaio/certs/ca.crt server.crt            # Verify chain
openssl s_client -connect localhost:9000                                 # Test handshake

```text

---

## Storage Issues

### Disk Space

```bash

df -h /var/lib/nebulaio/*                                    # Check volumes
du -sh /var/lib/nebulaio/data/* | sort -rh | head -10        # Large items
nebulaio admin cleanup --temp --older-than=24h               # Clean temp files
nebulaio admin multipart cleanup --bucket=mybucket --older-than=7d

```bash

### Data Integrity

```bash

nebulaio admin object verify --bucket=mybucket --key=mykey   # Verify object
nebulaio admin bucket verify --bucket=mybucket               # Scan bucket
nebulaio admin object heal --bucket=mybucket --key=mykey     # Heal corrupted

```bash

### Disk Health

```bash

smartctl -H /dev/nvme0n1                                     # SMART status
dmesg | grep -E "(nvme|sda|error)"                           # Disk errors
nebulaio admin disk replace --old=/dev/sda --new=/dev/sdb    # Replace disk

```text

---

## Cluster Health Problems

### Status Checks

```bash

nebulaio cluster status        # Overall health
nebulaio cluster members       # List nodes
nebulaio cluster raft status   # Consensus state

```bash

### Node Connectivity

```bash

for node in node1 node2 node3; do
  echo -n "$node: "; nc -zv $node 9003 2>&1 | grep -o "succeeded\|failed"
done

```bash

### Quorum Recovery

```bash

nebulaio cluster quorum status
nebulaio cluster recover --force-new-cluster  # Use cautiously

```bash

### Replication Issues

```bash

nebulaio cluster replication status
nebulaio admin bucket replication resync --bucket=mybucket
nebulaio cluster node resync --node=node2

```text

---

## Performance Issues

### Identify Bottlenecks

```bash

vmstat 1 5                                                           # System stats
iostat -x 1 5                                                        # Disk I/O
curl -s http://localhost:9001/api/v1/metrics | grep nebulaio_        # Metrics
curl -o cpu.pprof http://localhost:6060/debug/pprof/profile?seconds=30

```bash

### Network Performance

```bash

iperf3 -c storage-node -t 10           # Bandwidth test
ping -c 100 storage-node | tail -2     # Packet loss

```bash

### Quick Fixes

```yaml

# config.yaml optimizations
s3:
  connection_pool: { max_idle: 200, max_open: 2000 }
  multipart: { threshold: 64MB, part_size: 32MB }
cache: { enabled: true, size: 4GB }

```text

---

## Getting Support

### Generate Support Bundle

```bash

nebulaio diag bundle --output=/tmp/nebulaio-support.tar.gz

```bash

### Before Contacting Support

1. Gather version: `nebulaio version`
2. Note deployment type (standalone/cluster/Kubernetes)
3. Collect relevant logs
4. Document reproduction steps

### Support Channels

- **GitHub Issues:** Bug reports and feature requests
- **GitHub Discussions:** Community Q&A

### Bug Report Template

```markdown

**Version:** (nebulaio version)
**Deployment:** Standalone / Docker / Kubernetes
**OS:** Ubuntu 22.04 / RHEL 9 / etc.
**Description:** Brief issue description
**Steps:** 1. Step one  2. Step two
**Expected:** What should happen
**Actual:** What actually happens
**Logs:** (sanitized excerpts)

```

---

## Related Documentation

- [Performance Tuning](../PERFORMANCE_TUNING.md)
- [Clustering Guide](../architecture/clustering.md)
- [Configuration Reference](../getting-started/configuration.md)
