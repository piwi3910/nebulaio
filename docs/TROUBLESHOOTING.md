# NebulaIO Troubleshooting Guide

This guide helps diagnose and resolve common issues with NebulaIO deployments.

## Table of Contents

1. [Quick Diagnostics](#quick-diagnostics)
2. [Installation Issues](#installation-issues)
3. [Startup Problems](#startup-problems)
4. [Connection Issues](#connection-issues)
5. [Performance Problems](#performance-problems)
6. [S3 API Errors](#s3-api-errors)
7. [Cluster Issues](#cluster-issues)
8. [Storage Issues](#storage-issues)
9. [AI/ML Feature Issues](#aiml-feature-issues)
10. [Security Issues](#security-issues)
11. [Web Console Issues](#web-console-issues)
12. [Logging and Debugging](#logging-and-debugging)

---

## Quick Diagnostics

### Health Check Commands

```bash
# Check service status
systemctl status nebulaio

# Quick health check
curl -s http://localhost:9000/minio/health/live

# Detailed health check
curl -s http://localhost:9001/api/v1/health | jq

# Cluster status
nebulaio cluster status

# System diagnostics
nebulaio diag --all
```

### Log Locations

| Component | Log Location |
|-----------|--------------|
| NebulaIO Server | `/var/log/nebulaio/server.log` |
| API Gateway | `/var/log/nebulaio/api.log` |
| Cluster | `/var/log/nebulaio/cluster.log` |
| Audit | `/var/log/nebulaio/audit.log` |
| Web Console | Browser developer console |

---

## Installation Issues

### Issue: Binary Not Found After Installation

**Symptoms:**
```
bash: nebulaio: command not found
```

**Solutions:**

1. Check installation path:
```bash
which nebulaio
ls -la /usr/local/bin/nebulaio
```

2. Add to PATH:
```bash
export PATH=$PATH:/usr/local/bin
echo 'export PATH=$PATH:/usr/local/bin' >> ~/.bashrc
```

3. Verify permissions:
```bash
chmod +x /usr/local/bin/nebulaio
```

### Issue: Dependency Missing

**Symptoms:**
```
error while loading shared libraries: libxxx.so: cannot open shared object file
```

**Solutions:**

1. Install missing dependencies:
```bash
# Ubuntu/Debian
apt-get update && apt-get install -y libc6 libssl3

# RHEL/CentOS
yum install -y glibc openssl-libs
```

2. Check library paths:
```bash
ldconfig -p | grep libssl
export LD_LIBRARY_PATH=/usr/local/lib:$LD_LIBRARY_PATH
```

### Issue: Configuration File Not Found

**Symptoms:**
```
Error: config file not found: /etc/nebulaio/config.yaml
```

**Solutions:**

1. Create configuration directory:
```bash
mkdir -p /etc/nebulaio
```

2. Copy sample configuration:
```bash
cp /usr/share/nebulaio/config.sample.yaml /etc/nebulaio/config.yaml
```

3. Use custom config path:
```bash
nebulaio server --config=/path/to/config.yaml
```

---

## Startup Problems

### Issue: Port Already in Use

**Symptoms:**
```
Error: listen tcp :9000: bind: address already in use
```

**Solutions:**

1. Find process using the port:
```bash
lsof -i :9000
netstat -tlnp | grep 9000
```

2. Kill the process or change port:
```bash
# Kill process
kill -9 $(lsof -t -i:9000)

# Or change port in config
# config.yaml
server:
  s3_port: 9002
  console_port: 9003
```

### Issue: Permission Denied

**Symptoms:**
```
Error: open /var/lib/nebulaio/data: permission denied
```

**Solutions:**

1. Check directory ownership:
```bash
ls -la /var/lib/nebulaio/
```

2. Fix permissions:
```bash
chown -R nebulaio:nebulaio /var/lib/nebulaio
chmod -R 755 /var/lib/nebulaio
```

3. Run with correct user:
```bash
sudo -u nebulaio nebulaio server
```

### Issue: TLS Certificate Errors

**Symptoms:**
```
Error: tls: failed to load certificate: open /etc/nebulaio/certs/server.crt: no such file
```

**Solutions:**

1. Generate self-signed certificates:
```bash
mkdir -p /etc/nebulaio/certs
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout /etc/nebulaio/certs/server.key \
  -out /etc/nebulaio/certs/server.crt \
  -subj "/CN=nebulaio.local"
```

2. Disable TLS for testing:
```yaml
# config.yaml
server:
  tls:
    enabled: false
```

### Issue: Out of Memory at Startup

**Symptoms:**
```
fatal error: runtime: out of memory
```

**Solutions:**

1. Check available memory:
```bash
free -h
```

2. Reduce memory allocation:
```yaml
# config.yaml
memory:
  cache_size: 2GB
  write_buffer:
    size: 512MB
```

3. Increase system memory or swap:
```bash
# Add swap
fallocate -l 4G /swapfile
chmod 600 /swapfile
mkswap /swapfile
swapon /swapfile
```

---

## Connection Issues

### Issue: Connection Refused

**Symptoms:**
```
Error: dial tcp 127.0.0.1:9000: connect: connection refused
```

**Solutions:**

1. Check if server is running:
```bash
systemctl status nebulaio
ps aux | grep nebulaio
```

2. Check firewall rules:
```bash
# UFW
ufw status
ufw allow 9000/tcp
ufw allow 9001/tcp

# firewalld
firewall-cmd --list-ports
firewall-cmd --permanent --add-port=9000/tcp
firewall-cmd --reload
```

3. Check bind address:
```yaml
# config.yaml
server:
  bind_address: "0.0.0.0"  # Not "127.0.0.1" for external access
```

### Issue: Connection Timeout

**Symptoms:**
```
Error: dial tcp 10.0.0.5:9000: i/o timeout
```

**Solutions:**

1. Test network connectivity:
```bash
ping 10.0.0.5
telnet 10.0.0.5 9000
nc -zv 10.0.0.5 9000
```

2. Check network routes:
```bash
traceroute 10.0.0.5
ip route show
```

3. Increase client timeout:
```bash
# AWS CLI
aws configure set default.s3.connect_timeout 60
```

### Issue: SSL/TLS Handshake Failure

**Symptoms:**
```
Error: tls: handshake failure
Error: x509: certificate signed by unknown authority
```

**Solutions:**

1. Skip certificate verification (testing only):
```bash
curl -k https://localhost:9000/
export AWS_CA_BUNDLE=/path/to/ca.crt
```

2. Install CA certificate:
```bash
cp /etc/nebulaio/certs/ca.crt /usr/local/share/ca-certificates/
update-ca-certificates
```

3. Use correct certificate:
```yaml
# config.yaml
server:
  tls:
    cert_file: /etc/nebulaio/certs/server.crt
    key_file: /etc/nebulaio/certs/server.key
    ca_file: /etc/nebulaio/certs/ca.crt
```

---

## Performance Problems

### Issue: Slow Upload/Download

**Symptoms:**
- Transfer speeds below expected
- High latency on operations

**Diagnosis:**

```bash
# Check network throughput
iperf3 -c server-ip

# Check disk I/O
iostat -x 1

# Check CPU usage
top -H -p $(pgrep nebulaio)

# Check system load
uptime
```

**Solutions:**

1. Increase multipart thresholds:
```yaml
# config.yaml
s3:
  multipart:
    threshold: 100MB
    part_size: 50MB
    concurrent_parts: 20
```

2. Enable compression:
```yaml
compression:
  enabled: true
  algorithm: zstd
  level: 3
```

3. Tune network settings:
```bash
# Increase TCP buffers
sysctl -w net.core.rmem_max=16777216
sysctl -w net.core.wmem_max=16777216
```

### Issue: High Memory Usage

**Symptoms:**
- OOM kills
- Swap usage increasing

**Diagnosis:**

```bash
# Check memory usage
ps aux --sort=-%mem | head
cat /proc/$(pgrep nebulaio)/status | grep -i mem

# Check for memory leaks
go tool pprof http://localhost:6060/debug/pprof/heap
```

**Solutions:**

1. Limit cache sizes:
```yaml
# config.yaml
memory:
  cache_size: 4GB
  max_memory: 8GB
```

2. Enable memory limits:
```bash
# systemd service
[Service]
MemoryMax=8G
MemoryHigh=6G
```

3. Tune garbage collection:
```bash
export GOGC=50
export GOMEMLIMIT=6GiB
```

### Issue: High CPU Usage

**Symptoms:**
- CPU constantly at 100%
- Slow response times

**Diagnosis:**

```bash
# CPU profile
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Check for hot goroutines
curl http://localhost:6060/debug/pprof/goroutine?debug=1

# System CPU breakdown
mpstat -P ALL 1
```

**Solutions:**

1. Adjust worker count:
```yaml
# config.yaml
cpu:
  workers: 16  # Match physical cores
```

2. Enable connection pooling:
```yaml
s3:
  connection_pool:
    max_idle: 100
    max_open: 1000
```

3. Disable expensive features:
```yaml
# Temporarily disable for diagnosis
features:
  compression: false
  encryption: false
```

---

## S3 API Errors

### Issue: AccessDenied (403)

**Symptoms:**
```xml
<Error>
  <Code>AccessDenied</Code>
  <Message>Access Denied</Message>
</Error>
```

**Solutions:**

1. Verify credentials:
```bash
# Check access key
nebulaio admin user info --username=myuser

# Regenerate access key
nebulaio admin user accesskey create --username=myuser
```

2. Check bucket policy:
```bash
nebulaio admin policy info --bucket=mybucket
```

3. Check IAM policy attached to user:
```bash
nebulaio admin user policy list --username=myuser
```

### Issue: NoSuchBucket (404)

**Symptoms:**
```xml
<Error>
  <Code>NoSuchBucket</Code>
  <Message>The specified bucket does not exist</Message>
</Error>
```

**Solutions:**

1. List available buckets:
```bash
aws --endpoint-url http://localhost:9000 s3 ls
```

2. Check bucket name spelling (case-sensitive):
```bash
nebulaio admin bucket list
```

3. Create the bucket:
```bash
aws --endpoint-url http://localhost:9000 s3 mb s3://mybucket
```

### Issue: SignatureDoesNotMatch

**Symptoms:**
```xml
<Error>
  <Code>SignatureDoesNotMatch</Code>
  <Message>The request signature we calculated does not match the signature you provided</Message>
</Error>
```

**Solutions:**

1. Verify secret key:
```bash
# Ensure no trailing whitespace
echo -n "mysecretkey" | xxd
```

2. Check system time:
```bash
# Time must be within 15 minutes
timedatectl
ntpdate -q pool.ntp.org
```

3. Check signature version:
```bash
# Use v4 signature
aws configure set default.s3.signature_version s3v4
```

### Issue: SlowDown (503)

**Symptoms:**
```xml
<Error>
  <Code>SlowDown</Code>
  <Message>Reduce your request rate</Message>
</Error>
```

**Solutions:**

1. Implement exponential backoff in client

2. Increase rate limits:
```yaml
# config.yaml
rate_limiting:
  enabled: true
  requests_per_second: 10000
  burst: 20000
```

3. Scale horizontally:
```bash
# Add more nodes to cluster
nebulaio cluster join --peer=node2:9000
```

---

## Cluster Issues

### Issue: Cluster Split Brain

**Symptoms:**
- Multiple leaders detected
- Inconsistent data between nodes

**Diagnosis:**

```bash
# Check cluster status on each node
nebulaio cluster status

# Check Raft state
nebulaio cluster raft status
```

**Solutions:**

1. Identify majority partition:
```bash
# Find partition with most nodes
for node in node1 node2 node3; do
  echo "$node: $(ssh $node nebulaio cluster members | wc -l)"
done
```

2. Stop minority partition nodes:
```bash
systemctl stop nebulaio
```

3. Restart with forced leader:
```bash
nebulaio cluster recover --force-leader
```

### Issue: Node Won't Join Cluster

**Symptoms:**
```
Error: failed to join cluster: dial tcp: connection refused
```

**Solutions:**

1. Check network connectivity between nodes:
```bash
nc -zv leader-node 9000
nc -zv leader-node 9001
```

2. Verify cluster token:
```bash
# Tokens must match
grep cluster_token /etc/nebulaio/config.yaml
```

3. Check TLS configuration:
```yaml
# All nodes must use same CA
cluster:
  tls:
    ca_file: /etc/nebulaio/certs/cluster-ca.crt
```

### Issue: Replication Lag

**Symptoms:**
- Data not appearing on replicas
- High replication queue

**Diagnosis:**

```bash
# Check replication status
nebulaio cluster replication status

# Check queue depth
curl -s http://localhost:9001/api/v1/metrics | grep replication_queue
```

**Solutions:**

1. Increase replication bandwidth:
```yaml
# config.yaml
replication:
  bandwidth_limit: 0  # Unlimited
  batch_size: 10000
```

2. Check network between nodes:
```bash
iperf3 -c replica-node
```

3. Clear and rebuild replica:
```bash
nebulaio cluster node resync --node=node2
```

---

## Storage Issues

### Issue: Disk Full

**Symptoms:**
```
Error: no space left on device
```

**Solutions:**

1. Check disk usage:
```bash
df -h
du -sh /var/lib/nebulaio/*
```

2. Clean up temporary files:
```bash
nebulaio admin cleanup --temp --older-than=24h
```

3. Enable lifecycle policies:
```yaml
# Bucket lifecycle rule
lifecycle:
  rules:
    - id: cleanup-old
      prefix: logs/
      expiration:
        days: 30
```

4. Move data to new volume:
```bash
# Add new storage volume
nebulaio admin storage add --path=/mnt/new-volume
nebulaio admin storage balance
```

### Issue: Data Corruption

**Symptoms:**
- Checksum mismatch errors
- Objects returning corrupted data

**Diagnosis:**

```bash
# Verify object integrity
nebulaio admin object verify --bucket=mybucket --key=mykey

# Scan bucket for corruption
nebulaio admin bucket verify --bucket=mybucket
```

**Solutions:**

1. Restore from erasure coding:
```bash
nebulaio admin object heal --bucket=mybucket --key=mykey
```

2. Restore from backup:
```bash
nebulaio restore --source=backup --bucket=mybucket
```

3. Check disk health:
```bash
smartctl -a /dev/nvme0n1
```

### Issue: I/O Errors

**Symptoms:**
```
Error: read /data/objects/xxx: input/output error
```

**Solutions:**

1. Check disk health:
```bash
dmesg | grep -i error
smartctl -H /dev/sda
```

2. Check filesystem:
```bash
# Unmount first if possible
xfs_repair /dev/sda1
# or
fsck.ext4 -y /dev/sda1
```

3. Replace failing disk:
```bash
nebulaio admin disk replace --old=/dev/sda --new=/dev/sdb
```

---

## AI/ML Feature Issues

### Issue: GPUDirect Not Working

**Symptoms:**
```
Error: GPUDirect storage not available
Warning: Falling back to CPU path
```

**Solutions:**

1. Check NVIDIA driver:
```bash
nvidia-smi
cat /proc/driver/nvidia/version
```

2. Check CUDA installation:
```bash
nvcc --version
ls /usr/local/cuda/lib64/
```

3. Verify GPUDirect support:
```bash
# Check for GDS driver
lsmod | grep nvidia_fs
modprobe nvidia_fs
```

4. Enable in configuration:
```yaml
# config.yaml
gpudirect:
  enabled: true
  devices: [0]
  fallback_to_cpu: true
```

### Issue: RDMA Connection Failed

**Symptoms:**
```
Error: RDMA device not found
Error: Cannot create queue pair
```

**Solutions:**

1. Check RDMA devices:
```bash
ibv_devices
ibv_devinfo
```

2. Load kernel modules:
```bash
modprobe ib_core
modprobe rdma_ucm
modprobe ib_uverbs
```

3. Install RDMA packages:
```bash
# Ubuntu
apt install rdma-core ibverbs-utils

# RHEL
yum install rdma-core libibverbs-utils
```

4. Configure network:
```bash
# For RoCE
rdma link add rxe0 type rxe netdev eth0
```

### Issue: S3 Express Session Errors

**Symptoms:**
```
Error: Session expired
Error: Maximum sessions exceeded
```

**Solutions:**

1. Check session limits:
```yaml
# config.yaml
s3_express:
  sessions:
    max_active: 10000
    timeout: 5m
```

2. Clean up stale sessions:
```bash
nebulaio admin s3express sessions cleanup
```

3. Monitor session usage:
```bash
curl -s http://localhost:9001/api/v1/metrics | grep s3express_sessions
```

---

## Security Issues

### Issue: Authentication Failures

**Symptoms:**
- Repeated 401/403 errors
- Login failures in web console

**Diagnosis:**

```bash
# Check auth logs
grep "auth" /var/log/nebulaio/server.log | tail -50

# Check for brute force
grep "failed" /var/log/nebulaio/audit.log | wc -l
```

**Solutions:**

1. Reset user password:
```bash
nebulaio admin user password reset --username=admin
```

2. Check account lockout:
```bash
nebulaio admin user info --username=admin
nebulaio admin user unlock --username=admin
```

3. Verify LDAP/OIDC configuration:
```bash
nebulaio admin identity test --provider=ldap
```

### Issue: Certificate Expired

**Symptoms:**
```
Error: x509: certificate has expired or is not yet valid
```

**Solutions:**

1. Check certificate dates:
```bash
openssl x509 -in /etc/nebulaio/certs/server.crt -noout -dates
```

2. Renew certificate:
```bash
# Self-signed
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout server.key -out server.crt

# Let's Encrypt
certbot renew
```

3. Reload service:
```bash
systemctl reload nebulaio
```

---

## Web Console Issues

### Issue: Console Not Loading

**Symptoms:**
- Blank page
- JavaScript errors

**Diagnosis:**

```bash
# Check console port
curl -I http://localhost:9001

# Check browser console
# Open Developer Tools > Console
```

**Solutions:**

1. Clear browser cache

2. Check CORS configuration:
```yaml
# config.yaml
console:
  cors:
    allowed_origins:
      - http://localhost:9001
      - https://console.example.com
```

3. Check static files:
```bash
ls -la /usr/share/nebulaio/console/
```

### Issue: Login Loop

**Symptoms:**
- Redirected back to login after logging in

**Solutions:**

1. Clear cookies and local storage

2. Check session configuration:
```yaml
# config.yaml
console:
  session:
    secure: false  # Set to true only with HTTPS
    same_site: lax
```

3. Check JWT settings:
```yaml
auth:
  jwt:
    secret: your-secret-key
    expiration: 24h
```

---

## Logging and Debugging

### Enable Debug Logging

```yaml
# config.yaml
logging:
  level: debug
  format: json
  output: /var/log/nebulaio/debug.log
```

Or at runtime:
```bash
nebulaio admin log level set debug
```

### Enable Request Tracing

```yaml
# config.yaml
tracing:
  enabled: true
  sample_rate: 1.0  # 100% for debugging
  otlp:
    endpoint: localhost:4317
```

### Generate Support Bundle

```bash
# Collect all diagnostic information
nebulaio diag bundle --output=/tmp/support-bundle.tar.gz

# Contents include:
# - Configuration (sanitized)
# - Recent logs
# - Metrics snapshot
# - System information
# - Cluster state
```

### Common Log Patterns

```bash
# Find errors
grep -i "error\|fail\|panic" /var/log/nebulaio/*.log

# Find slow requests
grep "duration_ms" /var/log/nebulaio/api.log | awk '$NF > 1000'

# Find authentication issues
grep "auth" /var/log/nebulaio/audit.log | grep -i "fail\|denied"

# Find cluster events
grep "raft\|leader\|join\|leave" /var/log/nebulaio/cluster.log
```

---

## Getting Help

If you cannot resolve an issue using this guide:

1. **Search existing issues:** Check GitHub Issues for similar problems
2. **Generate diagnostics:** Run `nebulaio diag bundle`
3. **Open an issue:** Include the support bundle (sanitized) with your report
4. **Community support:** Join our Discord or Slack channel

---

## Additional Resources

- [Performance Tuning Guide](./PERFORMANCE_TUNING.md)
- [API Reference](./api/openapi.yaml)
- [Deployment Guide](./DEPLOYMENT.md)
- [Security Best Practices](./SECURITY.md)
