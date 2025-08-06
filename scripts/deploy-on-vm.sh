#!/bin/bash

# Vault Sync Operator Deployment Script for VM
# This script deploys the operator step by step with proper configuration

set -e

# Configuration variables
VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
VAULT_ROLE="${VAULT_ROLE:-vault-sync-operator}"
NAMESPACE="vault-sync-operator-system"

echo "🚀 Deploying Vault Sync Operator..."
echo "   Vault Address: $VAULT_ADDR"
echo "   Vault Role: $VAULT_ROLE"
echo "   Namespace: $NAMESPACE"
echo ""

# Function to wait for resources
wait_for_resource() {
    local resource_type="$1"
    local resource_name="$2"
    local namespace="${3:-}"
    
    echo "⏳ Waiting for $resource_type/$resource_name to be ready..."
    
    if [ -n "$namespace" ]; then
        kubectl wait --for=condition=available --timeout=300s "$resource_type/$resource_name" -n "$namespace" 2>/dev/null || true
    else
        kubectl wait --for=condition=established --timeout=300s "$resource_type/$resource_name" 2>/dev/null || true
    fi
}

# Step 1: Apply everything using the default kustomization
echo "📦 Step 1: Applying all resources using kustomize..."
kubectl apply -k config/default/
echo "✅ All resources applied"
echo ""

# Step 2: Wait for deployment to be ready
echo "⏳ Step 2: Waiting for deployment to be ready..."
wait_for_resource "deployment" "vault-sync-operator-controller-manager" "$NAMESPACE"
echo "✅ Deployment ready"
echo ""

# Step 4: Patch deployment with Vault configuration
echo "⚙️  Step 4: Configuring Vault connection..."
kubectl patch deployment vault-sync-operator-controller-manager \
  -n "$NAMESPACE" \
  --type='merge' \
  -p="{
    \"spec\": {
      \"template\": {
        \"spec\": {
          \"containers\": [
            {
              \"name\": \"manager\",
              \"env\": [
                {
                  \"name\": \"VAULT_ADDR\",
                  \"value\": \"$VAULT_ADDR\"
                },
                {
                  \"name\": \"VAULT_ROLE\",
                  \"value\": \"$VAULT_ROLE\"
                }
              ]
            }
          ]
        }
      }
    }
  }"
echo "✅ Vault configuration applied"
echo ""

# Step 5: Wait for updated deployment to be ready
echo "⏳ Step 5: Waiting for updated deployment to be ready..."
kubectl rollout status deployment/vault-sync-operator-controller-manager -n "$NAMESPACE"
echo "✅ Updated deployment ready"
echo ""

# Step 6: Check deployment status
echo "📊 Step 6: Checking deployment status..."
kubectl get all -n "$NAMESPACE"
echo ""

# Step 7: Show logs
echo "📋 Step 7: Recent operator logs..."
kubectl logs -n "$NAMESPACE" -l control-plane=controller-manager --tail=20
echo ""

echo "🎉 Deployment complete!"
echo ""
echo "🔧 Next steps:"
echo "1. Verify Vault role is configured:"
echo "   vault write auth/kubernetes/role/$VAULT_ROLE \\"
echo "       bound_service_account_names=vault-sync-operator-controller-manager \\"
echo "       bound_service_account_namespaces=$NAMESPACE \\"
echo "       policies=vault-sync-operator \\"
echo "       ttl=24h"
echo ""
echo "2. Test with a VaultSync resource:"
echo "   kubectl apply -f - <<EOF"
echo "   apiVersion: vault.example.com/v1alpha1"
echo "   kind: VaultSync"
echo "   metadata:"
echo "     name: test-sync"
echo "     namespace: default"
echo "   spec:"
echo "     vaultPath: \"secret/test-app\""
echo "     secretName: \"test-app-secret\""
echo "     refreshInterval: \"30s\""
echo "   EOF"
echo ""
echo "3. Monitor logs:"
echo "   kubectl logs -n $NAMESPACE -l control-plane=controller-manager -f"
