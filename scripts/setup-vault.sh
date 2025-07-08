#!/bin/bash

# setup-vault.sh - Script to configure Vault for the vault-sync-operator

set -e

# Configuration
VAULT_ADDR=${VAULT_ADDR:-"http://localhost:8200"}
VAULT_TOKEN=${VAULT_TOKEN:-""}
KUBERNETES_HOST=${KUBERNETES_HOST:-"https://kubernetes.default.svc"}
SERVICE_ACCOUNT_NAME=${SERVICE_ACCOUNT_NAME:-"vault-sync-operator-controller-manager"}
NAMESPACE=${NAMESPACE:-"vault-sync-operator-system"}
VAULT_ROLE=${VAULT_ROLE:-"vault-sync-operator"}
VAULT_AUTH_PATH=${VAULT_AUTH_PATH:-"kubernetes"}

echo "Setting up Vault for vault-sync-operator..."

# Check if vault CLI is available
if ! command -v vault &> /dev/null; then
    echo "Error: vault CLI is not installed or not in PATH"
    exit 1
fi

# Set vault address
export VAULT_ADDR

# Authenticate if token is provided
if [ -n "$VAULT_TOKEN" ]; then
    export VAULT_TOKEN
fi

echo "1. Enabling Kubernetes auth backend..."
vault auth enable -path="$VAULT_AUTH_PATH" kubernetes || echo "Kubernetes auth already enabled"

echo "2. Getting Kubernetes cluster information..."
# Get the service account token and CA cert
if kubectl get serviceaccount "$SERVICE_ACCOUNT_NAME" -n "$NAMESPACE" &> /dev/null; then
    TOKEN_REVIEW_JWT=$(kubectl get secret $(kubectl get serviceaccount "$SERVICE_ACCOUNT_NAME" -n "$NAMESPACE" -o jsonpath='{.secrets[0].name}') -n "$NAMESPACE" -o jsonpath='{.data.token}' | base64 --decode)
    KUBE_CA_CERT=$(kubectl config view --raw --minify --flatten -o jsonpath='{.clusters[].cluster.certificate-authority-data}' | base64 --decode)
else
    echo "Warning: Service account $SERVICE_ACCOUNT_NAME not found in namespace $NAMESPACE"
    echo "You may need to deploy the operator first, then run this script"
    TOKEN_REVIEW_JWT=""
    KUBE_CA_CERT=""
fi

echo "3. Configuring Kubernetes auth backend..."
if [ -n "$TOKEN_REVIEW_JWT" ] && [ -n "$KUBE_CA_CERT" ]; then
    vault write "auth/$VAULT_AUTH_PATH/config" \
        token_reviewer_jwt="$TOKEN_REVIEW_JWT" \
        kubernetes_host="$KUBERNETES_HOST" \
        kubernetes_ca_cert="$KUBE_CA_CERT"
else
    echo "Skipping Kubernetes auth config due to missing service account"
fi

echo "4. Creating Vault policy for the operator..."
cat << EOF | vault policy write vault-sync-operator -
# Allow reading and writing secrets
path "secret/data/*" {
  capabilities = ["create", "update", "delete", "read"]
}

# Allow listing secrets
path "secret/metadata/*" {
  capabilities = ["list"]
}

# Allow token renewal
path "auth/token/renew-self" {
  capabilities = ["update"]
}

# Allow token lookup
path "auth/token/lookup-self" {
  capabilities = ["read"]
}
EOF

echo "5. Creating Kubernetes auth role..."
vault write "auth/$VAULT_AUTH_PATH/role/$VAULT_ROLE" \
    bound_service_account_names="$SERVICE_ACCOUNT_NAME" \
    bound_service_account_namespaces="$NAMESPACE" \
    policies="vault-sync-operator" \
    ttl=24h

echo "6. Testing authentication (if service account exists)..."
if [ -n "$TOKEN_REVIEW_JWT" ]; then
    echo "Testing login with service account token..."
    vault write "auth/$VAULT_AUTH_PATH/login" role="$VAULT_ROLE" jwt="$TOKEN_REVIEW_JWT" || echo "Login test failed - this is expected if the operator is not yet deployed"
fi

echo ""
echo "Vault setup complete!"
echo ""
echo "Configuration summary:"
echo "  Vault Address: $VAULT_ADDR"
echo "  Auth Path: $VAULT_AUTH_PATH"
echo "  Role: $VAULT_ROLE"
echo "  Service Account: $SERVICE_ACCOUNT_NAME"
echo "  Namespace: $NAMESPACE"
echo ""
echo "Next steps:"
echo "1. Deploy the operator: make deploy"
echo "2. Create secrets and deployments with vault-sync annotations"
echo "3. Check operator logs: kubectl logs -n $NAMESPACE deployment/vault-sync-operator-controller-manager"
