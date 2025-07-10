#!/bin/bash

# Script to build and validate manifests locally without kubectl apply
# This generates the manifests and checks for potential issues

set -e

echo "🔍 Validating Vault Sync Operator manifests..."

# Create temp directory for validation
TEMP_DIR=$(mktemp -d)
echo "Using temp directory: $TEMP_DIR"

# Function to check if a file has duplicate resource names
check_duplicates() {
    local file=$1
    echo "Checking $file for duplicate resources..."
    
    # Extract all resource names and kinds
    cat "$file" | grep -E "^(kind|  name):" | while read line; do
        if [[ $line == kind:* ]]; then
            current_kind=$(echo "$line" | cut -d: -f2 | tr -d ' ')
        elif [[ $line == *name:* ]]; then
            current_name=$(echo "$line" | cut -d: -f2 | tr -d ' ')
            echo "$current_kind/$current_name"
        fi
    done | sort | uniq -c | sort -nr | while read count resource; do
        if [[ $count -gt 1 ]]; then
            echo "⚠️  WARNING: Duplicate resource found: $resource (appears $count times)"
        fi
    done
}

# Generate manifests for each component
echo "📦 Generating CRD manifests..."
kustomize build config/crd/ > "$TEMP_DIR/crd.yaml"
check_duplicates "$TEMP_DIR/crd.yaml"

echo "📦 Generating RBAC manifests..."
kustomize build config/rbac/ > "$TEMP_DIR/rbac.yaml"
check_duplicates "$TEMP_DIR/rbac.yaml"

echo "📦 Generating Manager manifests..."
kustomize build config/manager/ > "$TEMP_DIR/manager.yaml"
check_duplicates "$TEMP_DIR/manager.yaml"

echo "📦 Generating Default (all-in-one) manifests..."
kustomize build config/default/ > "$TEMP_DIR/default.yaml"
check_duplicates "$TEMP_DIR/default.yaml"

# Check for YAML syntax errors
echo "🔍 Checking YAML syntax..."
for file in "$TEMP_DIR"/*.yaml; do
    if python3 -c "import yaml; yaml.safe_load(open('$file'))" 2>/dev/null; then
        echo "✅ $file: Valid YAML"
    else
        echo "❌ $file: Invalid YAML"
    fi
done

# Create a deployment package
PACKAGE_DIR="vault-sync-operator-manifests-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$PACKAGE_DIR"

# Copy individual manifests
cp "$TEMP_DIR"/*.yaml "$PACKAGE_DIR/"

# Copy original config directories for reference
cp -r config/crd "$PACKAGE_DIR/"
cp -r config/rbac "$PACKAGE_DIR/"
cp -r config/manager "$PACKAGE_DIR/"
cp -r config/default "$PACKAGE_DIR/"

# Create deployment instructions
cat > "$PACKAGE_DIR/DEPLOY_ON_VM.md" << 'EOF'
# Deployment Instructions for VM

## Prerequisites
- k3s cluster running
- Vault configured with Kubernetes auth

## Step-by-step deployment

### 1. Create namespace first
```bash
kubectl apply -f default/namespace.yaml
```

### 2. Apply CRDs
```bash
kubectl apply -f crd.yaml
```

### 3. Apply RBAC
```bash
kubectl apply -f rbac.yaml
```

### 4. Apply Manager
```bash
kubectl apply -f manager.yaml
```

### Alternative: Use kustomize
```bash
kubectl apply -k default/
```

### Alternative: Apply all at once
```bash
kubectl apply -f default.yaml
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

### Patch deployment for local Vault:
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
                  "value": "http://YOUR_VM_IP:8200"
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

## Testing

### Create a test secret in Vault:
```bash
vault kv put secret/test-app \
    username=testuser \
    password=testpass123
```

### Create a VaultSync resource:
```bash
cat <<EOF | kubectl apply -f -
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

### Verify the secret was created:
```bash
kubectl get secret test-app-secret -o yaml
```
EOF

# Create tarball
tar -czf "$PACKAGE_DIR.tar.gz" "$PACKAGE_DIR"

echo ""
echo "✅ Validation complete!"
echo "📦 Package created: $PACKAGE_DIR.tar.gz"
echo "📝 Transfer this to your VM and follow the instructions in DEPLOY_ON_VM.md"

# Cleanup temp directory
rm -rf "$TEMP_DIR"

echo ""
echo "🚀 Ready for deployment on your VM!"
