# Multi-Cluster Vault Sync Operator Deployment Guide

This guide shows how to deploy the vault-sync-operator across multiple clusters using the standard per-cluster deployment pattern.

## Architecture Overview

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   Cluster A     │    │   Cluster B     │    │   Cluster C     │
│                 │    │                 │    │                 │
│ ┌─────────────┐ │    │ ┌─────────────┐ │    │ ┌─────────────┐ │
│ │   Operator  │ │    │ │   Operator  │ │    │ │   Operator  │ │
│ │             │ │    │ │             │ │    │ │             │ │
│ └─────────────┘ │    │ └─────────────┘ │    │ └─────────────┘ │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                    ┌─────────────────┐
                    │  Shared Vault   │
                    │    Server       │
                    └─────────────────┘
```

## Benefits of Per-Cluster Deployment

1. **Security Isolation**: Each cluster's secrets remain within cluster boundaries
2. **Failure Isolation**: One cluster's issues don't affect others
3. **Simple Networking**: No cross-cluster networking complexity
4. **Standard Practice**: Follows Kubernetes operator conventions
5. **Independent Operations**: Each cluster can be managed independently

## Deployment Steps

### 1. Vault Configuration

Configure Vault with separate auth backends for each cluster:

```bash
# Enable Kubernetes auth for each cluster
vault auth enable -path=kubernetes-cluster-a kubernetes
vault auth enable -path=kubernetes-cluster-b kubernetes  
vault auth enable -path=kubernetes-cluster-c kubernetes

# Configure each auth backend with cluster-specific settings
vault write auth/kubernetes-cluster-a/config \
    token_reviewer_jwt="$(cat /cluster-a-sa-token)" \
    kubernetes_host="https://cluster-a-api:443" \
    kubernetes_ca_cert=@/cluster-a-ca.crt

vault write auth/kubernetes-cluster-b/config \
    token_reviewer_jwt="$(cat /cluster-b-sa-token)" \
    kubernetes_host="https://cluster-b-api:443" \
    kubernetes_ca_cert=@/cluster-b-ca.crt

# Create cluster-specific policies
vault policy write vault-sync-cluster-a - <<EOF
path "clusters/cluster-a/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
EOF

vault policy write vault-sync-cluster-b - <<EOF
path "clusters/cluster-b/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}
EOF

# Create roles for each cluster
vault write auth/kubernetes-cluster-a/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-cluster-a \
    ttl=24h

vault write auth/kubernetes-cluster-b/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-cluster-b \
    ttl=24h
```

### 2. Deploy to Each Cluster

#### Cluster A Deployment

```yaml
# cluster-a-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vault-sync-operator-controller-manager
  namespace: vault-sync-operator-system
spec:
  replicas: 1
  selector:
    matchLabels:
      control-plane: controller-manager
  template:
    metadata:
      labels:
        control-plane: controller-manager
    spec:
      containers:
      - name: manager
        image: vault-sync-operator:latest
        args:
        - --leader-elect
        - --vault-addr=$(VAULT_ADDR)
        - --vault-role=vault-sync-operator
        - --vault-auth-path=kubernetes-cluster-a  # Cluster-specific auth path
        - --cluster-name=cluster-a                # Cluster identifier
        env:
        - name: VAULT_ADDR
          value: "https://vault.company.com:8200"
        - name: GOMEMLIMIT
          valueFrom:
            resourceFieldRef:
              resource: limits.memory
        resources:
          limits:
            cpu: 500m
            memory: 128Mi
          requests:
            cpu: 10m
            memory: 64Mi
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop: ["ALL"]
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8081
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8081
      serviceAccountName: vault-sync-operator-controller-manager
      securityContext:
        runAsNonRoot: true
```

#### Cluster B Deployment

```yaml
# cluster-b-deployment.yaml
# Same as cluster-a but with different values:
        args:
        - --vault-auth-path=kubernetes-cluster-b
        - --cluster-name=cluster-b
```

### 3. Application Deployment Examples

#### Cluster A Application

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    # Will be stored at: clusters/cluster-a/secret/data/my-app
spec:
  # ... deployment spec
```

#### Cluster B Application  

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    # Will be stored at: clusters/cluster-b/secret/data/my-app
spec:
  # ... deployment spec
```

## Vault Path Organization

With cluster-aware paths, secrets are organized as:

```
vault/
├── clusters/
│   ├── cluster-a/
│   │   ├── secret/data/my-app
│   │   ├── secret/data/database
│   │   └── secret/data/api-keys
│   ├── cluster-b/
│   │   ├── secret/data/my-app
│   │   ├── secret/data/frontend
│   │   └── secret/data/backend
│   └── cluster-c/
│       ├── secret/data/analytics
│       └── secret/data/monitoring
```

This prevents path conflicts between clusters while maintaining clear organization.

## Monitoring Across Clusters

Each operator instance exposes its own metrics endpoint. Use Prometheus federation or a centralized monitoring solution:

```yaml
# prometheus-config.yaml
scrape_configs:
- job_name: 'vault-sync-cluster-a'
  kubernetes_sd_configs:
  - role: pod
    kubeconfig_file: /cluster-a-kubeconfig
  relabel_configs:
  - source_labels: [__meta_kubernetes_pod_label_control_plane]
    action: keep
    regex: controller-manager
  - source_labels: [__meta_kubernetes_pod_container_port_name]
    action: keep
    regex: metrics

- job_name: 'vault-sync-cluster-b'
  kubernetes_sd_configs:
  - role: pod
    kubeconfig_file: /cluster-b-kubeconfig
  # ... similar config
```

## Troubleshooting Multi-Cluster Setup

### Common Issues

1. **Auth Backend Mismatch**: Ensure `--vault-auth-path` matches the Vault auth backend name
2. **Policy Conflicts**: Verify each cluster has appropriate Vault policies
3. **Path Conflicts**: Use different cluster names or path prefixes
4. **Network Access**: Ensure all clusters can reach the Vault server

### Verification Commands

```bash
# Check operator logs in each cluster
kubectl logs -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager

# Verify Vault paths from each cluster
vault kv list clusters/cluster-a/secret/data/
vault kv list clusters/cluster-b/secret/data/

# Check metrics from each cluster
kubectl port-forward -n vault-sync-operator-system svc/vault-sync-operator-controller-manager-metrics-service 8080:8080
curl http://localhost:8080/metrics | grep vault_sync_operator
```

## Best Practices

1. **Consistent Naming**: Use consistent cluster names across environments
2. **Policy Isolation**: Each cluster should only access its own Vault paths
3. **Monitoring**: Monitor each operator instance independently
4. **Backup Strategy**: Include cluster context in backup/restore procedures
5. **Documentation**: Maintain cluster-specific configuration documentation
