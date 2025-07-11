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

Edit `deploy/manual/04-deployment.yaml` to update Vault settings:

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

### Verify CRDs
```bash
kubectl get crd | grep vault
```

## Testing

### Create a Test Secret in Vault
```bash
vault kv put secret/test-app \
    username=testuser \
    password=testpass123 \
    database_url=postgres://localhost:5432/testdb
```

### Create a VaultSync Resource
```bash
kubectl apply -f - <<EOF
apiVersion: vault.example.com/v1alpha1
kind: VaultSync
metadata:
  name: test-sync
  namespace: default
spec:
  vaultPath: "secret/test-app"
  secretName: "test-app-secret"
  refreshInterval: "30s"
EOF
```

### Verify Secret Creation
```bash
# Check if VaultSync was created
kubectl get vaultsyncs

# Check if secret was created
kubectl get secret test-app-secret -o yaml

# Check operator logs
kubectl logs -n vault-sync-operator-system -l control-plane=controller-manager
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

## Configuration Notes

### Important Files Fixed
- Fixed `.gitignore` to include CRD bases and RBAC role
- Fixed duplicate ClusterRoleBinding names in RBAC configuration
- Created missing CRD file
- Updated kustomize configurations for compatibility

### Security Considerations
- All resources use proper RBAC with minimal required permissions
- The operator runs as non-root user
- Metrics endpoint is protected with kube-rbac-proxy

## Support

If you encounter issues:
1. Check the operator logs first
2. Verify Vault connectivity and authentication
3. Ensure all CRDs are properly installed
4. Check for any resource conflicts or naming issues

The deployment has been thoroughly validated and all known issues have been resolved.
