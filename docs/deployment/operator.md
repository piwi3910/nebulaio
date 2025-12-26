# Kubernetes Operator Deployment

The NebulaIO Operator provides automated management of NebulaIO clusters on Kubernetes using Custom Resources.

## Benefits of Using the Operator

- **Declarative Management**: Define clusters as Kubernetes resources
- **Automated Operations**: Scaling, upgrades, and backups handled automatically
- **Self-Healing**: Automatic recovery from failures
- **GitOps Ready**: Manage clusters through version-controlled manifests

---

## Prerequisites

- Kubernetes 1.23+
- kubectl configured for your cluster
- (Optional) cert-manager for TLS
- (Optional) Prometheus Operator for monitoring

---

## Installation

### Install CRDs

```bash
kubectl apply -f https://raw.githubusercontent.com/piwi3910/nebulaio/main/deployments/operator/config/crd/bases/storage.nebulaio.io_nebulaios.yaml
kubectl apply -f https://raw.githubusercontent.com/piwi3910/nebulaio/main/deployments/operator/config/crd/bases/storage.nebulaio.io_nebulaiobackups.yaml
```

Or from local checkout:

```bash
kubectl apply -k deployments/operator/config/crd/bases/
```

### Install the Operator

```bash
kubectl apply -k https://github.com/piwi3910/nebulaio//deployments/operator/config/default
```

Or from local checkout:

```bash
kubectl apply -k deployments/operator/config/default/
```

### Verify Installation

```bash
# Check CRDs
kubectl get crd | grep nebulaio

# Check operator
kubectl get pods -n nebulaio-system
kubectl logs -n nebulaio-system deployment/nebulaio-operator
```

---

## Custom Resources

### NebulaIO

The `NebulaIO` custom resource defines a NebulaIO cluster.

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: my-cluster
  namespace: default
spec:
  replicas: 3
  image:
    repository: ghcr.io/piwi3910/nebulaio
    tag: latest
  cluster:
    name: my-cluster
    nodeRole: storage
  auth:
    rootUser: admin
    rootPasswordSecretRef:
      name: my-cluster-credentials
      key: password
  storage:
    storageClassName: fast-ssd
    size: 100Gi
  resources:
    requests:
      cpu: 1000m
      memory: 2Gi
    limits:
      cpu: 4000m
      memory: 8Gi
  monitoring:
    enabled: true
    serviceMonitor: true
  podDisruptionBudget:
    enabled: true
    minAvailable: 2
```

### NebulaIOBackup

The `NebulaIOBackup` custom resource defines backup configurations.

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIOBackup
metadata:
  name: daily-backup
  namespace: default
spec:
  clusterRef:
    name: my-cluster
  schedule: "0 2 * * *"  # Daily at 2 AM
  destination:
    type: s3
    s3:
      endpoint: https://s3.amazonaws.com
      bucket: my-backups
      prefix: nebulaio/
      region: us-east-1
      credentialsSecretRef:
        name: backup-credentials
  retention:
    keepLast: 7
    maxAge: 30d
```

---

## Deployment Examples

### Single Node {#single-node}

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: nebulaio-dev
  namespace: nebulaio
spec:
  replicas: 1
  auth:
    rootUser: admin
    rootPasswordSecretRef:
      name: nebulaio-credentials
      key: password
  storage:
    size: 50Gi
  monitoring:
    enabled: true
```

Deploy:

```bash
# Create namespace and credentials
kubectl create namespace nebulaio
kubectl create secret generic nebulaio-credentials \
  --namespace nebulaio \
  --from-literal=password=changeme

# Apply the CR
kubectl apply -f nebulaio-dev.yaml

# Watch the deployment
kubectl get nebulaio -n nebulaio -w
```

### HA Cluster {#ha-cluster}

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: nebulaio-ha
  namespace: nebulaio
spec:
  replicas: 3
  cluster:
    name: nebulaio-ha
    nodeRole: storage
  auth:
    rootUser: admin
    rootPasswordSecretRef:
      name: nebulaio-credentials
      key: password
  storage:
    storageClassName: fast-ssd
    size: 100Gi
  resources:
    requests:
      cpu: 1000m
      memory: 2Gi
    limits:
      cpu: 4000m
      memory: 8Gi
  service:
    type: ClusterIP
  monitoring:
    enabled: true
    serviceMonitor: true
  podDisruptionBudget:
    enabled: true
    minAvailable: 2
```

### Production Cluster {#production}

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: nebulaio-production
  namespace: nebulaio-production
spec:
  replicas: 5
  image:
    repository: ghcr.io/piwi3910/nebulaio
    tag: v1.0.0
    pullPolicy: Always
  cluster:
    name: nebulaio-production
    nodeRole: storage
  auth:
    rootUser: admin
    rootPasswordSecretRef:
      name: nebulaio-production-credentials
      key: password
  storage:
    storageClassName: fast-ssd
    size: 500Gi
  resources:
    requests:
      cpu: 2000m
      memory: 4Gi
    limits:
      cpu: 8000m
      memory: 16Gi
  service:
    type: LoadBalancer
    annotations:
      service.beta.kubernetes.io/aws-load-balancer-type: nlb
  ingress:
    enabled: true
    className: nginx
    annotations:
      nginx.ingress.kubernetes.io/proxy-body-size: "5g"
      cert-manager.io/cluster-issuer: letsencrypt-prod
    s3Host: s3.production.example.com
    adminHost: admin.production.example.com
    consoleHost: console.production.example.com
    tls:
      enabled: true
      secretName: nebulaio-production-tls
  monitoring:
    enabled: true
    serviceMonitor: true
    prometheusRules: true
  podDisruptionBudget:
    enabled: true
    minAvailable: 3
  nodeSelector:
    node-role.kubernetes.io/storage: "true"
  tolerations:
    - key: node-role.kubernetes.io/storage
      operator: Exists
      effect: NoSchedule
```

---

## Separate Management and Storage Planes

For large-scale deployments, use separate management and storage plane clusters:

### Management Plane

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: nebulaio-management
  namespace: nebulaio-system
spec:
  replicas: 3
  cluster:
    name: nebulaio-cluster
    nodeRole: management
  storage:
    size: 10Gi  # Small - metadata only
  resources:
    requests:
      cpu: 500m
      memory: 1Gi
    limits:
      cpu: 2000m
      memory: 4Gi
  nodeSelector:
    node-role.kubernetes.io/control-plane: ""
```

### Storage Plane

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIO
metadata:
  name: nebulaio-storage
  namespace: nebulaio-system
spec:
  replicas: 10  # Scale independently
  cluster:
    name: nebulaio-cluster
    nodeRole: storage
  storage:
    storageClassName: nvme-fast
    size: 1Ti
  resources:
    requests:
      cpu: 4000m
      memory: 8Gi
    limits:
      cpu: 16000m
      memory: 32Gi
  nodeSelector:
    node-role.kubernetes.io/storage: "true"
```

---

## Operations

### Scaling

```bash
# Scale up
kubectl patch nebulaio my-cluster --type merge -p '{"spec":{"replicas":5}}'

# Scale down (maintain Raft quorum)
kubectl patch nebulaio my-cluster --type merge -p '{"spec":{"replicas":3}}'
```

### Upgrading

```bash
# Update image tag
kubectl patch nebulaio my-cluster --type merge \
  -p '{"spec":{"image":{"tag":"v1.1.0"}}}'
```

### Backup

```yaml
# One-time backup
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIOBackup
metadata:
  name: manual-backup-$(date +%Y%m%d)
spec:
  clusterRef:
    name: my-cluster
  destination:
    type: s3
    s3:
      bucket: my-backups
      prefix: manual/
      credentialsSecretRef:
        name: backup-credentials
```

Apply:

```bash
kubectl apply -f backup.yaml
kubectl get nebulaiobackup -w
```

### Scheduled Backups

```yaml
apiVersion: storage.nebulaio.io/v1alpha1
kind: NebulaIOBackup
metadata:
  name: scheduled-backup
spec:
  clusterRef:
    name: my-cluster
  schedule: "0 */6 * * *"  # Every 6 hours
  destination:
    type: s3
    s3:
      bucket: my-backups
      prefix: scheduled/
      credentialsSecretRef:
        name: backup-credentials
  retention:
    keepLast: 10
    maxAge: 7d
```

---

## Status Monitoring

### Check Cluster Status

```bash
kubectl get nebulaio my-cluster -o yaml
```

Status fields:
- `phase`: Current phase (Pending, Creating, Running, Updating, Failed)
- `replicas`: Desired replicas
- `readyReplicas`: Ready replicas
- `leader`: Current Raft leader
- `conditions`: Detailed conditions

### Watch Status

```bash
kubectl get nebulaio -w
```

### Check Events

```bash
kubectl get events --field-selector involvedObject.name=my-cluster
```

---

## Troubleshooting

### Operator Logs

```bash
kubectl logs -n nebulaio-system deployment/nebulaio-operator -f
```

### Cluster Pod Logs

```bash
kubectl logs -l app.kubernetes.io/instance=my-cluster -f
```

### Common Issues

**Cluster stuck in Creating**:
- Check storage class exists
- Verify node resources
- Check operator logs

**Backup failed**:
- Verify S3 credentials
- Check network connectivity to S3
- Verify bucket permissions

**Scaling issues**:
- Ensure Raft quorum maintained
- Check pod anti-affinity constraints
- Verify node capacity

---

## Uninstalling

### Delete Clusters

```bash
kubectl delete nebulaio --all -A
kubectl delete nebulaiobackup --all -A
```

### Delete Operator

```bash
kubectl delete -k deployments/operator/config/default/
```

### Delete CRDs

**Warning**: This removes all NebulaIO resources!

```bash
kubectl delete crd nebulaios.storage.nebulaio.io
kubectl delete crd nebulaiobackups.storage.nebulaio.io
```

---

## Next Steps

- [Monitoring Setup](../operations/monitoring.md)
- [Backup & Recovery](../operations/backup.md)
- [Scaling Guide](../operations/scaling.md)
