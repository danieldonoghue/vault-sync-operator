# Complete Vault Setup Guide for vault-sync-operator

This guide provides detailed instructions for setting up HashiCorp Vault to work with the vault-sync-operator, including specific considerations for k3s and VM deployments.

## Prerequisites

- HashiCorp Vault server running and accessible
- Kubernetes cluster (k3s or standard)
- vault-sync-operator deployed
- `vault` CLI tool installed and configured

## Initial Vault Setup

### 1. Initialize Vault (if not already done)

```bash
# Initialize Vault (only if running for the first time)
vault operator init

# Unseal Vault (required after restart)
vault operator unseal <unseal-key-1>
vault operator unseal <unseal-key-2>
vault operator unseal <unseal-key-3>

# Login with root token
vault login <root-token>
```

### 2. Enable KV Secrets Engine

```bash
# Check if KV engine is already enabled
vault secrets list

# Enable KV v2 engine (recommended)
vault secrets enable -version=2 kv

# Or enable at a custom path
vault secrets enable -path=secret -version=2 kv
```

### 3. Verify KV Engine

```bash
# Verify the KV engine is working
vault kv list secret/

# The operator will create secrets here when it syncs from Kubernetes
# No need to create test secrets manually since sync direction is K8s → Vault
```

## Prerequisites

Before configuring Vault's Kubernetes auth, ensure the vault-sync-operator is deployed:

### 1. Deploy the Operator

Choose one of the deployment methods:
- **Helm Chart**: `helm install vault-sync-operator charts/vault-sync-operator/`
- **Kustomize**: `kubectl apply -k config/default`
- **Manual**: `kubectl apply -f deploy/manual/`

All deployment methods now automatically create the required ServiceAccount token secret for Kubernetes 1.24+ compatibility.

### 2. Verify Deployment

```bash
# Check that the operator is running
kubectl get pods -n vault-sync-operator-system

# Check that the ServiceAccount exists
kubectl get serviceaccount vault-sync-operator-controller-manager -n vault-sync-operator-system

# Check that the token secret was created automatically
kubectl get secret -n vault-sync-operator-system | grep token

# Verify the token is populated (secret name depends on deployment method)
TOKEN_SECRET=$(kubectl get secret -n vault-sync-operator-system -o name | grep token)
kubectl get $TOKEN_SECRET -n vault-sync-operator-system -o jsonpath='{.data.token}' | base64 -d | cut -c1-20
```

## Kubernetes Authentication Configuration

### Method 1: Configuration from Inside Cluster (Recommended)

This method is most reliable for k3s and VM deployments:

```bash
# Create a temporary pod with Vault CLI
kubectl run vault-config --image=vault:1.13.3 --rm -it --restart=Never -- sh

# Inside the pod, set Vault address
export VAULT_ADDR=http://your-vault-server:8200

# Authenticate with Vault
vault login <your-vault-token>

# Enable Kubernetes auth backend
vault auth enable kubernetes

# Configure the auth backend
vault write auth/kubernetes/config \
    kubernetes_host="https://kubernetes.default.svc.cluster.local" \
    kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
    token_reviewer_jwt="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)"
```

### Method 2: Configuration from Outside Cluster

If you prefer to configure from outside the cluster:

```bash
# Get cluster CA certificate from the ServiceAccount token secret
TOKEN_SECRET_NAME=$(kubectl get secret -n vault-sync-operator-system -o name | grep token | head -1)
kubectl get $TOKEN_SECRET_NAME -n vault-sync-operator-system -o jsonpath='{.data.ca\.crt}' | base64 -d > ca.crt

# Get service account token from the specific secret we created
SA_TOKEN=$(kubectl get $TOKEN_SECRET_NAME -n vault-sync-operator-system -o jsonpath='{.data.token}' | base64 -d)

# Get Kubernetes API server URL
KUBE_HOST=$(kubectl config view --raw --minify --flatten -o jsonpath='{.clusters[].cluster.server}')

# Configure Vault
vault write auth/kubernetes/config \
    kubernetes_host="$KUBE_HOST" \
    kubernetes_ca_cert=@ca.crt \
    token_reviewer_jwt="$SA_TOKEN"
```

### Method 3: k3s Specific Configuration

For k3s clusters, you might need to adjust the approach:

```bash
# k3s typically uses a different service account setup
# Get the k3s cluster's internal service address
KUBE_HOST="https://kubernetes.default.svc.cluster.local"

# For k3s, you might need to get the CA from the k3s config
sudo cat /etc/rancher/k3s/k3s.yaml | grep certificate-authority-data | awk '{print $2}' | base64 -d > k3s-ca.crt

# Configure Vault with k3s specifics
vault write auth/kubernetes/config \
    kubernetes_host="$KUBE_HOST" \
    kubernetes_ca_cert=@k3s-ca.crt \
    token_reviewer_jwt="$(kubectl get $(kubectl get secret -n vault-sync-operator-system -o name | grep token | head -1) -n vault-sync-operator-system -o jsonpath='{.data.token}' | base64 -d)"
```

## Policy Configuration

### Create Comprehensive Policy

```bash
# Create policy for vault-sync-operator
vault policy write vault-sync-operator - <<EOF
# For KV v2 engine (default)
path "secret/data/*" {
  capabilities = ["create", "update", "delete", "read"]
}

# Allow listing and reading secrets
path "secret/metadata/*" {
  capabilities = ["list", "read"]
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
```

### Create Role

```bash
# Create role that binds service account to policy
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=24h \
    max_ttl=24h
```

## Verification and Testing

### 1. Verify Configuration

```bash
# Check auth backend status
vault auth list | grep kubernetes

# Check configuration
vault read auth/kubernetes/config

# Check role
vault read auth/kubernetes/role/vault-sync-operator

# Check policy
vault policy read vault-sync-operator
```

### 2. Test Authentication

```bash
# Get service account token from the specific secret we created
TOKEN_SECRET_NAME=$(kubectl get secret -n vault-sync-operator-system -o name | grep token | head -1)
SA_TOKEN=$(kubectl get $TOKEN_SECRET_NAME -n vault-sync-operator-system -o jsonpath='{.data.token}' | base64 -d)

# Test login
vault write auth/kubernetes/login role=vault-sync-operator jwt="$SA_TOKEN"
```

Expected output:
```
Key                                       Value
---                                       -----
token                                     s.xxxxxxxxxxxxxxxxxxxxx
token_accessor                            xxxxxxxxxxxxxxxxxxxxx
token_duration                            24h
token_renewable                           true
token_policies                            ["default" "vault-sync-operator"]
identity_policies                         []
policies                                  ["default" "vault-sync-operator"]
```

### 3. Test Secret Access

```bash
# Use the token from the previous step
export VAULT_TOKEN=s.xxxxxxxxxxxxxxxxxxxxx

# Test that the operator can write to the secret path
# (this will be empty initially since sync direction is K8s → Vault)
vault kv list secret/

# The operator will populate secrets here when Kubernetes deployments
# with vault-sync annotations are created
```

## VM and k3s Specific Considerations

### Network Configuration

For VM deployments, ensure:

```bash
# Test connectivity from operator to Vault
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  curl -s http://your-vault-server:8200/v1/sys/health

# Check if Vault is accessible
kubectl run network-test --image=busybox --rm -it --restart=Never -- \
  wget -qO- http://your-vault-server:8200/v1/sys/health
```

### Firewall Rules

Ensure the following ports are open:
- Vault HTTP: 8200
- Vault HTTPS: 8201 (if using TLS)
- Kubernetes API: 6443 (standard) or 6444 (k3s)

### k3s Service Account Tokens

k3s handles service account tokens differently:

```bash
# Check token availability in k3s
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  ls -la /var/run/secrets/kubernetes.io/serviceaccount/

# Verify token content
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  cat /var/run/secrets/kubernetes.io/serviceaccount/token | cut -c1-20
```

## Troubleshooting Common Issues

### Issue 1: "unsupported path" Error

```bash
# Check if you're using the right KV engine version
vault secrets list -detailed | grep secret/

# For KV v1, paths should be: secret/path
# For KV v2, paths should be: secret/data/path (for data) and secret/metadata/path (for metadata)
```

### Issue 2: "permission denied" on Authentication

```bash
# Check service account exists
kubectl get serviceaccount vault-sync-operator-controller-manager -n vault-sync-operator-system

# Check if the specific token secret exists
kubectl get secret -n vault-sync-operator-system | grep token

# Verify role configuration
vault read auth/kubernetes/role/vault-sync-operator
```

### Issue 3: "invalid jwt" Error

```bash
# Check token format from the specific secret
TOKEN_SECRET_NAME=$(kubectl get secret -n vault-sync-operator-system -o name | grep token | head -1)
SA_TOKEN=$(kubectl get $TOKEN_SECRET_NAME -n vault-sync-operator-system -o jsonpath='{.data.token}' | base64 -d)
echo $SA_TOKEN | cut -c1-20

# Verify token is valid JWT
echo $SA_TOKEN | cut -d'.' -f1 | base64 -d 2>/dev/null || echo "Invalid JWT header"
```

### Issue 4: Connection Timeouts

```bash
# Test basic connectivity
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  nc -zv your-vault-server 8200

# Check DNS resolution
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  nslookup your-vault-server
```

## Security Best Practices

### 1. Use TLS

```bash
# Enable TLS for Vault (recommended for production)
vault write sys/config/cors enabled=true allowed_origins="*"
```

### 2. Limit Token TTL

```bash
# Set appropriate TTL for tokens
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=1h \
    max_ttl=24h
```

### 3. Use Specific Secret Paths

```bash
# Create more restrictive policies
vault policy write vault-sync-operator - <<EOF
# Only allow access to specific application secrets
path "secret/data/app1/*" {
  capabilities = ["create", "update", "delete", "read"]
}
path "secret/data/app2/*" {
  capabilities = ["create", "update", "delete", "read"]
}
path "secret/metadata/app1/*" {
  capabilities = ["list", "read"]
}
path "secret/metadata/app2/*" {
  capabilities = ["list", "read"]
}
EOF
```

## Monitoring and Maintenance

### Enable Audit Logging

```bash
# Enable audit logging
vault audit enable file file_path=/vault/logs/audit.log

# Check audit logs
tail -f /vault/logs/audit.log | grep vault-sync-operator
```

### Monitor Token Usage

```bash
# Check token usage
vault token lookup -accessor <accessor>

# List active tokens
vault list auth/token/accessors
```

## Complete Setup Script

Here's a complete script for setting up Vault authentication:

```bash
#!/bin/bash
set -e

# Configuration
VAULT_ADDR="http://your-vault-server:8200"
VAULT_TOKEN="your-vault-token"
OPERATOR_NAMESPACE="vault-sync-operator-system"
OPERATOR_SA="vault-sync-operator-controller-manager"

echo "Setting up Vault authentication for vault-sync-operator..."

# Ensure the operator is deployed first (this creates the required ServiceAccount token secret)
echo "Checking if operator is deployed..."
if ! kubectl get deployment vault-sync-operator-controller-manager -n $OPERATOR_NAMESPACE >/dev/null 2>&1; then
  echo "ERROR: vault-sync-operator is not deployed. Please deploy it first using one of:"
  echo "  - Helm: helm install vault-sync-operator charts/vault-sync-operator/"
  echo "  - Kustomize: kubectl apply -k config/default"
  echo "  - Manual: kubectl apply -f deploy/manual/"
  exit 1
fi

# Find the ServiceAccount token secret
echo "Finding ServiceAccount token secret..."
TOKEN_SECRET_NAME=$(kubectl get secret -n $OPERATOR_NAMESPACE -o name 2>/dev/null | grep token | head -1 | cut -d/ -f2)
if [ -z "$TOKEN_SECRET_NAME" ]; then
  echo "ERROR: ServiceAccount token secret not found. Check operator deployment."
  exit 1
fi
echo "Found token secret: $TOKEN_SECRET_NAME"

# Set Vault address and token
export VAULT_ADDR=$VAULT_ADDR
export VAULT_TOKEN=$VAULT_TOKEN

# Enable Kubernetes auth backend
vault auth enable kubernetes

# Configure auth backend
# Note: This temporary pod uses its own built-in service account token for configuration,
# which is different from the operator's token secret we created above
kubectl run vault-config --image=vault:1.13.3 --rm -it --restart=Never -- sh -c "
export VAULT_ADDR=$VAULT_ADDR
export VAULT_TOKEN=$VAULT_TOKEN
vault write auth/kubernetes/config \
    kubernetes_host='https://kubernetes.default.svc.cluster.local' \
    kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
    token_reviewer_jwt=\"\$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)\"
"

# Create policy
vault policy write vault-sync-operator - <<EOF
# For KV v2 engine (default)
path "secret/data/*" {
  capabilities = ["create", "update", "delete", "read"]
}

# Allow listing and reading secrets
path "secret/metadata/*" {
  capabilities = ["list", "read"]
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

# Create role
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=$OPERATOR_SA \
    bound_service_account_namespaces=$OPERATOR_NAMESPACE \
    policies=vault-sync-operator \
    ttl=24h

# Test authentication
echo "Testing authentication..."
SA_TOKEN=$(kubectl get secret $TOKEN_SECRET_NAME -n $OPERATOR_NAMESPACE -o jsonpath='{.data.token}' | base64 -d)
vault write auth/kubernetes/login role=vault-sync-operator jwt="$SA_TOKEN"

echo "Setup complete!"
```

Save this as `setup-vault.sh` and run it after adjusting the configuration variables.

For additional troubleshooting, see [VAULT-AUTH-TROUBLESHOOTING.md](VAULT-AUTH-TROUBLESHOOTING.md).