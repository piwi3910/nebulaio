# Helm Chart Deployment

This guide covers deploying NebulaIO using the Helm chart.

## Prerequisites

- Kubernetes 1.23+
- Helm 3.8+
- PV provisioner in the cluster
- (Optional) Ingress controller
- (Optional) cert-manager for TLS
- (Optional) Prometheus Operator for monitoring

---

## Installation

### From Helm Repository

```bash
# Add the NebulaIO Helm repository
helm repo add nebulaio https://piwi3910.github.io/nebulaio/charts
helm repo update

# Install with default values
helm install nebulaio nebulaio/nebulaio

# Install with custom namespace
helm install nebulaio nebulaio/nebulaio --namespace nebulaio --create-namespace
```

### From Local Chart

```bash
cd deployments/helm

# Install with default values
helm install nebulaio ./nebulaio

# Install with custom values
helm install nebulaio ./nebulaio -f my-values.yaml
```

---

## Deployment Scenarios

### Single Node (Development) {#single-node}

```bash
helm install nebulaio ./nebulaio -f values-single-node.yaml
```

Or with inline values:

```bash
helm install nebulaio ./nebulaio \
  --set replicaCount=1 \
  --set cluster.enabled=false \
  --set persistence.size=50Gi
```

### HA Cluster (3 nodes) {#ha-cluster}

```bash
helm install nebulaio ./nebulaio -f values-ha.yaml
```

Or with inline values:

```bash
helm install nebulaio ./nebulaio \
  --set replicaCount=3 \
  --set cluster.enabled=true \
  --set podAntiAffinityPreset=hard \
  --set podDisruptionBudget.minAvailable=2
```

### Production (5 nodes) {#production}

First, create the credentials secret:

```bash
kubectl create namespace nebulaio-production

kubectl create secret generic nebulaio-credentials \
  --namespace nebulaio-production \
  --from-literal=root-user=admin \
  --from-literal=root-password=$(openssl rand -base64 32)
```

Then install:

```bash
helm install nebulaio ./nebulaio \
  --namespace nebulaio-production \
  -f values-production.yaml
```

---

## Configuration Reference

### Essential Parameters

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of nodes | `1` |
| `image.repository` | Image repository | `ghcr.io/piwi3910/nebulaio` |
| `image.tag` | Image tag | Chart appVersion |
| `auth.rootUser` | Admin username | `admin` |
| `auth.rootPassword` | Admin password (min 12 chars, mixed case, number) | `CHANGE_ME_Min12Chars1` |
| `auth.existingSecret` | Use existing secret (recommended for production) | `""` |

### Cluster Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `cluster.enabled` | Enable clustering | `true` |
| `cluster.name` | Cluster name | `nebulaio-cluster` |
| `cluster.nodeRole` | Node role | `storage` |

### Storage Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `persistence.enabled` | Enable persistence | `true` |
| `persistence.storageClass` | Storage class | `""` |
| `persistence.size` | Volume size | `10Gi` |
| `persistence.accessMode` | Access mode | `ReadWriteOnce` |

### Service Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `service.type` | Service type | `ClusterIP` |
| `service.s3Port` | S3 API port | `9000` |
| `service.adminPort` | Admin API port | `9001` |
| `service.consolePort` | Console port | `9002` |

### Ingress Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class | `nginx` |
| `ingress.s3.host` | S3 API hostname | `s3.example.com` |
| `ingress.admin.host` | Admin hostname | `admin.example.com` |
| `ingress.console.host` | Console hostname | `console.example.com` |
| `ingress.tls` | TLS configuration | `[]` |

### Resource Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `256Mi` |
| `resources.limits.cpu` | CPU limit | `2000m` |
| `resources.limits.memory` | Memory limit | `2Gi` |

### High Availability

| Parameter | Description | Default |
|-----------|-------------|---------|
| `podAntiAffinityPreset` | Anti-affinity: soft/hard | `soft` |
| `podDisruptionBudget.enabled` | Enable PDB | `true` |
| `podDisruptionBudget.minAvailable` | Min available pods | `""` |
| `podDisruptionBudget.maxUnavailable` | Max unavailable | `1` |

### Monitoring

| Parameter | Description | Default |
|-----------|-------------|---------|
| `metrics.enabled` | Enable metrics | `true` |
| `metrics.serviceMonitor.enabled` | Create ServiceMonitor | `false` |
| `metrics.prometheusRule.enabled` | Create PrometheusRule | `false` |
| `metrics.prometheusRule.defaultRules` | Enable default alerts | `true` |

### Network Policy

| Parameter | Description | Default |
|-----------|-------------|---------|
| `networkPolicy.enabled` | Enable network policy | `false` |
| `networkPolicy.allowS3FromAnywhere` | Open S3 API | `true` |
| `networkPolicy.adminAllowedNamespaces` | Admin access namespaces | `[]` |

---

## Example Values Files

### values-single-node.yaml

```yaml
replicaCount: 1

cluster:
  enabled: false

persistence:
  size: 50Gi

resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 2000m
    memory: 4Gi

podDisruptionBudget:
  enabled: false
```

### values-ha.yaml

```yaml
replicaCount: 3

cluster:
  enabled: true
  name: nebulaio-ha

persistence:
  size: 100Gi

resources:
  requests:
    cpu: 1000m
    memory: 2Gi
  limits:
    cpu: 4000m
    memory: 8Gi

podAntiAffinityPreset: hard

podDisruptionBudget:
  enabled: true
  minAvailable: 2

metrics:
  serviceMonitor:
    enabled: true
```

### values-production.yaml

```yaml
replicaCount: 5

auth:
  existingSecret: nebulaio-credentials

cluster:
  enabled: true
  name: nebulaio-production

persistence:
  storageClass: fast-ssd
  size: 500Gi

resources:
  requests:
    cpu: 2000m
    memory: 4Gi
  limits:
    cpu: 8000m
    memory: 16Gi

podAntiAffinityPreset: hard

nodeSelector:
  node-role.kubernetes.io/storage: "true"

ingress:
  enabled: true
  className: nginx
  annotations:
    nginx.ingress.kubernetes.io/proxy-body-size: "5g"
    cert-manager.io/cluster-issuer: letsencrypt-prod
  s3:
    host: s3.your-domain.com
  admin:
    host: admin.your-domain.com
  console:
    host: console.your-domain.com
  tls:
    - secretName: nebulaio-tls
      hosts:
        - s3.your-domain.com
        - admin.your-domain.com
        - console.your-domain.com

metrics:
  serviceMonitor:
    enabled: true
  prometheusRule:
    enabled: true

networkPolicy:
  enabled: true
```

---

## Separate Management and Storage Planes

For large deployments, deploy separate management and storage releases:

### Management Plane

```bash
helm install nebulaio-mgmt ./nebulaio \
  --namespace nebulaio-system \
  --set replicaCount=3 \
  --set cluster.nodeRole=management \
  --set persistence.size=10Gi \
  --set nodeSelector."node-role\.kubernetes\.io/control-plane"=""
```

### Storage Plane

```bash
helm install nebulaio-storage ./nebulaio \
  --namespace nebulaio-system \
  --set replicaCount=10 \
  --set cluster.nodeRole=storage \
  --set persistence.size=500Gi \
  --set persistence.storageClass=fast-ssd \
  --set nodeSelector."node-role\.kubernetes\.io/storage"="true"
```

---

## Upgrading

### Minor Version Upgrade

```bash
helm upgrade nebulaio ./nebulaio -f my-values.yaml
```

### Image Upgrade

```bash
helm upgrade nebulaio ./nebulaio \
  --set image.tag=v1.1.0 \
  --reuse-values
```

### Check Upgrade Status

```bash
helm status nebulaio
kubectl rollout status statefulset/nebulaio
```

---

## Scaling

### Scale Up

```bash
helm upgrade nebulaio ./nebulaio \
  --set replicaCount=5 \
  --reuse-values
```

### Scale Down

```bash
# Ensure you maintain Raft quorum (majority)
helm upgrade nebulaio ./nebulaio \
  --set replicaCount=3 \
  --reuse-values
```

---

## Uninstalling

```bash
helm uninstall nebulaio
```

**Note**: PVCs are not deleted automatically. To remove all data:

```bash
kubectl delete pvc -l app.kubernetes.io/instance=nebulaio
```

---

## Troubleshooting

### View Rendered Templates

```bash
helm template nebulaio ./nebulaio -f my-values.yaml
```

### Check Release Status

```bash
helm status nebulaio
helm history nebulaio
```

### Rollback

```bash
# Rollback to previous revision
helm rollback nebulaio

# Rollback to specific revision
helm rollback nebulaio 2
```

### Debug Installation

```bash
helm install nebulaio ./nebulaio --dry-run --debug
```

---

## Next Steps

- [Operator Deployment](operator.md) - Automated cluster management
- [Monitoring Setup](../operations/monitoring.md)
- [Backup & Recovery](../operations/backup.md)
