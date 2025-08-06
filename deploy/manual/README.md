# Manual Deployment Guide

This directory contains individual YAML files for manually deploying the Vault Sync Operator using kubectl.

## Prerequisites

- Kubernetes cluster running
- kubectl configured
- HashiCorp Vault installed and configured
- Vault Kubernetes authentication enabled

## Deployment Steps

Apply the manifests in the correct order:

```bash
# 1. Create namespace
kubectl apply -f 00-namespace.yaml

# 2. Create service account
kubectl apply -f 01-serviceaccount.yaml

# 3. Create RBAC resources
kubectl apply -f 02-rbac.yaml

# 4. Create deployment
kubectl apply -f 03-deployment.yaml

# 5. Create service
kubectl apply -f 04-service.yaml
```

Or apply all at once:

```bash
kubectl apply -f . --recursive
```

## Configuration

### Vault Settings

**⚠️ Important**: The default configuration assumes Vault is running inside the cluster at `http://vault:8200`. For external Vault servers, you must update the VAULT_ADDR.

Edit the `04-deployment.yaml` file to configure Vault settings:

```yaml
env:
- name: VAULT_ADDR
  value: "http://192.168.1.100:8200"     # Change to your Vault server address
- name: VAULT_ROLE
  value: "vault-sync-operator"            # Change to your Vault role
- name: VAULT_AUTH_PATH
  value: "kubernetes"                     # Change to your auth path
```

Common examples:
- VM deployment: `http://192.168.1.100:8200`
- External host: `http://vault.example.com:8200`
- HTTPS: `https://vault.example.com:8200`

### Vault Role Configuration

Ensure your Vault role is configured correctly:

```bash
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=24h
```

## Verification

Check deployment status:

```bash
kubectl get all -n vault-sync-operator-system
```

Check operator logs:

```bash
kubectl logs -n vault-sync-operator-system -l control-plane=controller-manager -f
```

## Uninstallation

To remove the operator:

```bash
# Remove in reverse order
kubectl delete -f 04-service.yaml
kubectl delete -f 03-deployment.yaml
kubectl delete -f 02-rbac.yaml
kubectl delete -f 01-serviceaccount.yaml
kubectl delete -f 00-namespace.yaml
```

Or remove all at once:

```bash
kubectl delete -f . --recursive
```

## Testing

This operator uses deployment annotations. Create a test deployment with vault-sync annotations:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
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
        - name: SECRET_VALUE
          valueFrom:
            secretKeyRef:
              name: app-secret
              key: password
```

The operator will automatically discover and sync the `app-secret` to Vault at `secret/data/test-app/app-secret`.