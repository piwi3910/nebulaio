# NebulaIO Helm Chart

This Helm chart deploys NebulaIO, an S3-compatible object storage system, on Kubernetes.

## Prerequisites

- Kubernetes 1.23+
- Helm 3.8+
- PV provisioner support in the underlying infrastructure (for persistence)

## Installing the Chart

### Add the Helm repository

```bash

helm repo add nebulaio https://piwi3910.github.io/nebulaio/charts
helm repo update

```bash

### Install with default configuration

```bash

helm install nebulaio nebulaio/nebulaio

```bash

### Install from local chart

```bash

cd deployments/helm
helm install nebulaio ./nebulaio

```bash

## Configuration

### Quick Start Examples

#### Single Node

```bash

helm install nebulaio ./nebulaio -f values-single-node.yaml

```bash

#### High Availability (3 nodes)

```bash

helm install nebulaio ./nebulaio -f values-ha.yaml

```bash

#### Production

```bash

# First, create the credentials secret
kubectl create secret generic nebulaio-credentials \
  --from-literal=root-user=admin \
  --from-literal=root-password=your-secure-password

# Install with production values
helm install nebulaio ./nebulaio -f values-production.yaml

```bash

### Parameters

#### Global Parameters

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Image repository | `ghcr.io/piwi3910/nebulaio` |
| `image.tag` | Image tag | `""` (uses Chart appVersion) |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `nameOverride` | Override chart name | `""` |
| `fullnameOverride` | Override full name | `""` |

#### Authentication

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `auth.rootUser` | Root admin username | `admin` |
| `auth.rootPassword` | Root admin password | `changeme` |
| `auth.existingSecret` | Use existing secret | `""` |
| `auth.existingSecretUserKey` | Key for username in secret | `root-user` |
| `auth.existingSecretPasswordKey` | Key for password in secret | `root-password` |
| `auth.jwtSecret` | JWT signing secret | `""` (auto-generated) |

#### Cluster Configuration

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `cluster.enabled` | Enable clustering | `true` |
| `cluster.name` | Cluster name | `nebulaio-cluster` |
| `cluster.nodeRole` | Node role: storage, management, hybrid | `storage` |
| `cluster.expectNodes` | Expected node count | `""` (uses replicaCount) |

#### Service Configuration

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `service.type` | Service type | `ClusterIP` |
| `service.s3Port` | S3 API port | `9000` |
| `service.adminPort` | Admin API port | `9001` |
| `service.consolePort` | Console port | `9002` |
| `service.annotations` | Service annotations | `{}` |

#### Ingress Configuration

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `ingress.enabled` | Enable ingress | `false` |
| `ingress.className` | Ingress class name | `nginx` |
| `ingress.annotations` | Ingress annotations | `{}` |
| `ingress.s3.host` | S3 API hostname | `s3.example.com` |
| `ingress.admin.host` | Admin API hostname | `admin.example.com` |
| `ingress.console.host` | Console hostname | `console.example.com` |
| `ingress.tls` | TLS configuration | `[]` |

#### Persistence

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `persistence.enabled` | Enable persistence | `true` |
| `persistence.storageClass` | Storage class | `""` (default) |
| `persistence.accessMode` | Access mode | `ReadWriteOnce` |
| `persistence.size` | Storage size | `10Gi` |

#### Resources

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `resources.requests.cpu` | CPU request | `100m` |
| `resources.requests.memory` | Memory request | `256Mi` |
| `resources.limits.cpu` | CPU limit | `2000m` |
| `resources.limits.memory` | Memory limit | `2Gi` |

#### Pod Configuration

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `podAntiAffinityPreset` | Pod anti-affinity: soft, hard | `soft` |
| `nodeSelector` | Node selector | `{}` |
| `tolerations` | Tolerations | `[]` |
| `affinity` | Custom affinity rules | `{}` |

#### Pod Disruption Budget

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `podDisruptionBudget.enabled` | Enable PDB | `true` |
| `podDisruptionBudget.minAvailable` | Minimum available | `""` |
| `podDisruptionBudget.maxUnavailable` | Maximum unavailable | `1` |

#### Monitoring

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `metrics.enabled` | Enable metrics | `true` |
| `metrics.serviceMonitor.enabled` | Enable ServiceMonitor | `false` |
| `metrics.serviceMonitor.interval` | Scrape interval | `30s` |
| `metrics.prometheusRule.enabled` | Enable PrometheusRule | `false` |
| `metrics.prometheusRule.defaultRules` | Enable default alerts | `true` |

#### Network Policy

| Parameter | Description | Default |
| ----------- | ------------- | --------- |
| `networkPolicy.enabled` | Enable network policy | `false` |
| `networkPolicy.allowS3FromAnywhere` | Allow S3 from anywhere | `true` |
| `networkPolicy.adminAllowedNamespaces` | Namespaces for admin access | `[]` |

## Deployment Scenarios

### Single Node (Development/Testing)

```yaml

# values-single-node.yaml
replicaCount: 1
cluster:
  enabled: false
persistence:
  size: 50Gi

```bash

### High Availability (Production)

```yaml

# values-ha.yaml
replicaCount: 3
cluster:
  enabled: true
podAntiAffinityPreset: hard
podDisruptionBudget:
  enabled: true
  minAvailable: 2

```bash

### Separate Management and Storage Planes

For large deployments, you can separate the management plane (Raft voters) from the storage plane:

```bash

# Deploy management nodes (3 nodes for Raft consensus)
helm install nebulaio-mgmt ./nebulaio \
  --set replicaCount=3 \
  --set cluster.nodeRole=management \
  --set persistence.size=10Gi

# Deploy storage nodes (scale independently)
helm install nebulaio-storage ./nebulaio \
  --set replicaCount=10 \
  --set cluster.nodeRole=storage \
  --set persistence.size=500Gi

```bash

## Upgrading

```bash

helm upgrade nebulaio ./nebulaio -f your-values.yaml

```bash

## Uninstalling

```bash

helm uninstall nebulaio

```text

**Note**: PVCs are not automatically deleted. To remove all data:

```bash

kubectl delete pvc -l app.kubernetes.io/instance=nebulaio

```bash

## Troubleshooting

### Check pod status

```bash

kubectl get pods -l app.kubernetes.io/name=nebulaio

```bash

### View logs

```bash

kubectl logs -l app.kubernetes.io/name=nebulaio -f

```bash

### Check cluster status

```bash

kubectl exec nebulaio-0 -- curl -s http://localhost:9001/api/v1/admin/cluster/nodes | jq

```bash

### Check health

```bash

kubectl exec nebulaio-0 -- curl -s http://localhost:9001/health/ready

```

## License

Apache License 2.0
