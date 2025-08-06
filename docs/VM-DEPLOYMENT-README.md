# Vault Sync Operator - VM Deployment Guide

## Overview
This guide helps you deploy the Vault Sync Operator on your Debian 12 VM with k3s using multiple deployment methods.

## Prerequisites
- Debian 12 VM with k3s running
- Vault installed and configured
- kubectl configured to access your k3s cluster
- Vault authentication configured for Kubernetes

## Deployment Methods

### Method 1: Helm Chart (Recommended)

Create a custom values file for your VM:

```yaml
# vm-values.yaml
vault:
  address: "http://192.168.1.100:8200"  # Replace with your VM's IP
  role: "vault-sync-operator"
  authPath: "kubernetes"

controllerManager:
  resources:
    limits:
      cpu: 500m
      memory: 128Mi
    requests:
      cpu: 10m
      memory: 64Mi
```

Deploy using Helm:

```bash
# Install Helm chart
helm install vault-sync-operator ./charts/vault-sync-operator \
  --namespace vault-sync-operator-system \
  --create-namespace \
  --values vm-values.yaml
```

### Method 2: Kustomize

**⚠️ Important**: You must update the VAULT_ADDR for your VM setup.

Edit `config/manager/manager.yaml` to update Vault address:

```yaml
env:
- name: VAULT_ADDR
  value: "http://192.168.1.100:8200"  # Replace with your VM's IP
- name: VAULT_ROLE
  value: "vault-sync-operator"
- name: VAULT_AUTH_PATH
  value: "kubernetes"
```

Deploy with kustomize:

```bash
# Apply using kustomize
kubectl apply -k config/default/
```

### Method 3: Manual kubectl

**⚠️ Important**: You must update the VAULT_ADDR for your VM setup.

Edit `deploy/manual/03-deployment.yaml` to update Vault settings:

```yaml
env:
- name: VAULT_ADDR
  value: "http://192.168.1.100:8200"  # Replace with your VM's IP
- name: VAULT_ROLE
  value: "vault-sync-operator"
- name: VAULT_AUTH_PATH
  value: "kubernetes"
```

Deploy manually:

```bash
# Apply all manual manifests
kubectl apply -f deploy/manual/ --recursive
```

### Method 4: Automated Script (Legacy)

```bash
# Set your Vault address (replace with your VM's IP)
export VAULT_ADDR="http://192.168.1.100:8200"

# Run the deployment script
./scripts/deploy-on-vm.sh
```

## Vault Configuration

### Update Vault Role
```bash
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=24h
```

## Verification

### Check Deployment Status
```bash
kubectl get all -n vault-sync-operator-system
```

### Check Logs
```bash
kubectl logs -n vault-sync-operator-system -l control-plane=controller-manager -f
```

### Verify Deployment
```bash
kubectl get pods -n vault-sync-operator-system
```

## Testing

### Create a Test Secret in Vault
```bash
vault kv put secret/test-app \
    username=testuser \
    password=testpass123 \
    database_url=postgres://localhost:5432/testdb
```

### Create a Test Deployment with Vault Sync
```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: test-app-secret
  namespace: default
type: Opaque
data:
  username: dGVzdHVzZXI=        # base64: testuser
  password: dGVzdHBhc3MxMjM=    # base64: testpass123
  database_url: cG9zdGdyZXM6Ly9sb2NhbGhvc3Q6NTQzMi90ZXN0ZGI=  # base64: postgres://localhost:5432/testdb
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  namespace: default
  annotations:
    vault-sync.io/path: "secret/data/test-app"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-app
  template:
    metadata:
      labels:
        app: test-app
    spec:
      containers:
      - name: app
        image: nginx:latest
        env:
        - name: DB_USER
          valueFrom:
            secretKeyRef:
              name: test-app-secret
              key: username
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: test-app-secret
              key: password
EOF
```

### Verify Secret Sync
```bash
# Check if deployment has vault-sync annotations
kubectl get deployment test-app -o yaml | grep "vault-sync.io"

# Check if secret exists
kubectl get secret test-app-secret -o yaml

# Check operator logs for sync activity
kubectl logs -n vault-sync-operator-system -l control-plane=controller-manager

# Verify secret was synced to Vault (from your VM)
vault kv get secret/data/test-app/test-app-secret
```

## Troubleshooting

### Common Issues

1. **Pod not starting**: Check logs and ensure Vault is accessible
   ```bash
   kubectl describe pod -n vault-sync-operator-system
   ```

2. **Authentication issues**: Verify Vault role configuration
   ```bash
   vault read auth/kubernetes/role/vault-sync-operator
   ```

3. **Permission issues**: Check RBAC configuration
   ```bash
   kubectl auth can-i --list --as=system:serviceaccount:vault-sync-operator-system:vault-sync-operator-controller-manager
   ```

### Debug Commands
```bash
# Check all resources
kubectl get all -n vault-sync-operator-system

# Check events
kubectl get events -n vault-sync-operator-system --sort-by='.lastTimestamp'

# Test Vault connectivity
kubectl run vault-test --image=vault:1.15.2 --rm -it -- vault version
```

## Security Considerations
- All resources use proper RBAC with minimal required permissions
- The operator runs as non-root user
- Metrics endpoint is protected with kube-rbac-proxy

## Support

If you encounter issues:
1. Check the operator logs first
2. Verify Vault connectivity and authentication
3. Verify the operator deployment is running correctly
4. Check for any resource conflicts or naming issues

The operator is ready for production use on VM environments.
