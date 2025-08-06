#!/bin/bash

# Script to build release manifests for deployment on VM
# Run this script to generate updated manifests after fixes

set -e

echo "Building updated manifests for VM deployment..."

# Create a temporary directory for the manifests
MANIFEST_DIR="vault-sync-operator-manifests-fixed"
rm -rf "$MANIFEST_DIR"
mkdir -p "$MANIFEST_DIR"

# Build each kustomization separately
echo "Building RBAC manifests..."
kubectl kustomize config/rbac/ > "$MANIFEST_DIR/rbac.yaml"

echo "Building manager manifests..."
kubectl kustomize config/manager/ > "$MANIFEST_DIR/manager.yaml"

echo "Building default manifests (all-in-one)..."
kubectl kustomize config/default/ > "$MANIFEST_DIR/all-in-one.yaml"

# Also copy individual directories for granular deployment
echo "Copying individual manifest directories..."
cp -r config/rbac "$MANIFEST_DIR/"
cp -r config/manager "$MANIFEST_DIR/"
cp -r config/default "$MANIFEST_DIR/"

# Create a namespace file
cat > "$MANIFEST_DIR/namespace.yaml" << 'EOF'
apiVersion: v1
kind: Namespace
metadata:
  labels:
    control-plane: controller-manager
    app.kubernetes.io/name: namespace
    app.kubernetes.io/instance: system
    app.kubernetes.io/component: manager
    app.kubernetes.io/created-by: vault-sync-operator
    app.kubernetes.io/part-of: vault-sync-operator
    app.kubernetes.io/managed-by: kustomize
  name: vault-sync-operator-system
EOF

# Create deployment instructions
cat > "$MANIFEST_DIR/DEPLOY.md" << 'EOF'
# Deployment Instructions

## Option 1: Deploy step by step (Recommended)
```bash
# 1. Create namespace
kubectl apply -f namespace.yaml

# 2. Apply RBAC
kubectl apply -f rbac.yaml

# 3. Apply manager
kubectl apply -f manager.yaml
```

## Option 2: Deploy all at once
```bash
kubectl apply -f all-in-one.yaml
```

## Option 3: Use kustomize (if preferred)
```bash
kubectl apply -k default/
```

## Post-deployment configuration

### Update Vault role:
```bash
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=24h
```

### Patch deployment for local Vault (replace IP with your VM's IP):
```bash
kubectl patch deployment vault-sync-operator-controller-manager \
  -n vault-sync-operator-system \
  --type='merge' \
  -p='{
    "spec": {
      "template": {
        "spec": {
          "containers": [
            {
              "name": "manager",
              "env": [
                {
                  "name": "VAULT_ADDR",
                  "value": "http://192.168.1.100:8200"
                },
                {
                  "name": "VAULT_ROLE",
                  "value": "vault-sync-operator"
                }
              ]
            }
          ]
        }
      }
    }
  }'
```

### Verify deployment:
```bash
kubectl get all -n vault-sync-operator-system
kubectl logs -n vault-sync-operator-system -l control-plane=controller-manager -f
```
EOF

# Create a tarball for easy transfer
tar -czf "$MANIFEST_DIR.tar.gz" "$MANIFEST_DIR"

echo "✅ Manifests built successfully!"
echo "📁 Directory: $MANIFEST_DIR"
echo "📦 Tarball: $MANIFEST_DIR.tar.gz"
echo ""
echo "To deploy on your VM:"
echo "1. Copy $MANIFEST_DIR.tar.gz to your VM"
echo "2. Extract: tar -xzf $MANIFEST_DIR.tar.gz"
echo "3. Follow instructions in $MANIFEST_DIR/DEPLOY.md"
