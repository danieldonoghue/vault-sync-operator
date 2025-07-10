# Vault Kubernetes Authentication Troubleshooting Guide

This guide helps diagnose and resolve Vault authentication issues, particularly "permission denied" errors when the operator tries to authenticate with Vault.

## Quick Diagnosis

If you're getting "permission denied" errors, start here:

```bash
# Check operator logs for specific error details
kubectl logs -n vault-sync-operator-system -l control-plane=controller-manager --tail=50 | grep -i "auth\|permission\|denied"
```

## Common Error Patterns

### 1. Authentication Failures
**Error**: `permission denied` during initial login
**Symptoms**: Operator cannot authenticate with Vault at all

### 2. Authorization Failures  
**Error**: `permission denied` during secret operations
**Symptoms**: Operator authenticates successfully but cannot read/write secrets

## Step-by-Step Troubleshooting

### Step 1: Verify Vault Connectivity

```bash
# Test if operator can reach Vault
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  curl -s $VAULT_ADDR/v1/sys/health

# Expected output should show Vault status
```

### Step 2: Check Kubernetes Authentication Backend

```bash
# Verify auth backend is enabled
vault auth list | grep kubernetes

# Check auth backend configuration
vault read auth/kubernetes/config

# Verify the configuration shows:
# - kubernetes_host: Should match your cluster API server
# - kubernetes_ca_cert: Should be present
# - token_reviewer_jwt: Should be configured
```

**Common Issues:**
- Missing `kubernetes_ca_cert`
- Incorrect `kubernetes_host` URL
- Invalid or expired `token_reviewer_jwt`

### Step 3: Validate Service Account Configuration

```bash
# Check if the service account exists
kubectl get serviceaccount vault-sync-operator-controller-manager -n vault-sync-operator-system

# Verify the service account has a token
kubectl get secret -n vault-sync-operator-system | grep vault-sync-operator-controller-manager

# Check token accessibility
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  ls -la /var/run/secrets/kubernetes.io/serviceaccount/
```

### Step 4: Test Manual Authentication

```bash
# Extract the service account token
SA_TOKEN=$(kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  cat /var/run/secrets/kubernetes.io/serviceaccount/token)

# Test authentication manually
vault write auth/kubernetes/login role=vault-sync-operator jwt="$SA_TOKEN"
```

**Expected Output:**
```
Key                                       Value
---                                       -----
token                                     s.xxxxxxxxxxxxxxxxxxxxx
token_accessor                            xxxxxxxxxxxxxxxxxxxxx
token_duration                            24h
token_renewable                           true
token_policies                            ["default" "vault-sync-operator"]
```

**If this fails, check:**
- Role name matches exactly: `vault-sync-operator`
- Service account name is correct in the role
- Namespace is correct in the role

### Step 5: Verify Vault Role Configuration

```bash
# Check the role configuration
vault read auth/kubernetes/role/vault-sync-operator
```

**Expected Output:**
```
Key                                      Value
---                                      -----
bound_service_account_names              [vault-sync-operator-controller-manager]
bound_service_account_namespaces         [vault-sync-operator-system]
policies                                 [vault-sync-operator]
ttl                                      24h
```

**Common Issues:**
- `bound_service_account_names` doesn't match actual service account name
- `bound_service_account_namespaces` doesn't match deployment namespace
- Missing or incorrect policies

### Step 6: Validate Policy Configuration

```bash
# Check if policy exists
vault policy read vault-sync-operator

# Test policy capabilities for common paths
vault policy test vault-sync-operator secret/data/test-path
```

**Expected Policy for KV v2:**
```hcl
path "secret/data/*" {
  capabilities = ["read"]
}
path "secret/metadata/*" {
  capabilities = ["read"]
}
```

**Expected Policy for KV v1:**
```hcl
path "secret/*" {
  capabilities = ["read"]
}
```

### Step 7: Test Secret Access

```bash
# First, authenticate and get a token
VAULT_TOKEN=$(vault write -field=token auth/kubernetes/login role=vault-sync-operator jwt="$SA_TOKEN")

# Test reading a secret (adjust path based on your KV engine version)
VAULT_TOKEN=$VAULT_TOKEN vault kv get secret/test-app

# Or for KV v1
VAULT_TOKEN=$VAULT_TOKEN vault read secret/test-app
```

## Common Misconfigurations

### 1. Service Account Name Mismatch

**Problem**: Role has wrong service account name
```bash
# Check actual service account name
kubectl get deployment vault-sync-operator-controller-manager -n vault-sync-operator-system -o jsonpath='{.spec.template.spec.serviceAccountName}'

# Update role if needed
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=24h
```

### 2. Namespace Mismatch

**Problem**: Role bound to wrong namespace
```bash
# Check actual namespace
kubectl get deployment vault-sync-operator-controller-manager -o jsonpath='{.metadata.namespace}'

# Update role if needed
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=24h
```

### 3. KV Engine Version Mismatch

**Problem**: Policy paths don't match KV engine version

```bash
# Check KV engine version
vault secrets list -detailed | grep secret/

# For KV v2, use these paths in policy:
path "secret/data/*" {
  capabilities = ["read"]
}
path "secret/metadata/*" {
  capabilities = ["read"]
}

# For KV v1, use these paths in policy:
path "secret/*" {
  capabilities = ["read"]
}
```

### 4. Incorrect Kubernetes Host Configuration

**Problem**: Auth backend configured with wrong Kubernetes API server

```bash
# Check current configuration
vault read auth/kubernetes/config

# Get correct Kubernetes host
kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}'

# Update configuration
vault write auth/kubernetes/config \
    kubernetes_host="https://your-correct-k8s-api-server:443" \
    kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
    token_reviewer_jwt="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)"
```

## Debugging Commands Reference

### Operator-Side Debugging

```bash
# Check operator logs
kubectl logs -n vault-sync-operator-system -l control-plane=controller-manager -f

# Check pod status
kubectl get pods -n vault-sync-operator-system

# Check service account details
kubectl describe serviceaccount vault-sync-operator-controller-manager -n vault-sync-operator-system

# Test connectivity from operator pod
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  curl -s $VAULT_ADDR/v1/sys/health

# Check environment variables
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- env | grep VAULT
```

### Vault-Side Debugging

```bash
# Check Vault status
vault status

# List auth methods
vault auth list

# Check audit logs (if enabled)
vault audit list

# Test token capabilities
vault token capabilities vault-sync-operator secret/data/test-path

# Check secret engines
vault secrets list

# Test manual KV operations
vault kv put secret/test-app username=test password=test
vault kv get secret/test-app
```

### Kubernetes-Side Debugging

```bash
# Check RBAC permissions
kubectl auth can-i get serviceaccount \
  --as=system:serviceaccount:vault-sync-operator-system:vault-sync-operator-controller-manager

# Check service account token
kubectl get secret -n vault-sync-operator-system | grep vault-sync-operator-controller-manager

# Verify deployment environment
kubectl describe deployment vault-sync-operator-controller-manager -n vault-sync-operator-system
```

## k3s Specific Considerations

For k3s clusters, additional considerations:

### 1. Service Account Token Location

```bash
# k3s might have different token paths
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  find /var/run/secrets -name "token" -type f
```

### 2. API Server Discovery

```bash
# Get k3s API server address
kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}'

# Verify from within cluster
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  printenv | grep KUBERNETES
```

### 3. Certificate Authority

```bash
# Check CA cert accessibility
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  ls -la /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
```

## Complete Working Example

Here's a complete, tested sequence for setting up Vault Kubernetes auth:

```bash
# 1. Enable Kubernetes auth (from within a pod that has access to service account)
vault auth enable kubernetes

# 2. Configure the auth backend
vault write auth/kubernetes/config \
    kubernetes_host="https://kubernetes.default.svc.cluster.local" \
    kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
    token_reviewer_jwt="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)"

# 3. Create policy
vault policy write vault-sync-operator - <<EOF
path "secret/data/*" {
  capabilities = ["read"]
}
path "secret/metadata/*" {
  capabilities = ["read"]
}
EOF

# 4. Create role
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=24h

# 5. Verify configuration
vault read auth/kubernetes/role/vault-sync-operator
vault policy read vault-sync-operator

# 6. Test authentication
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  vault write auth/kubernetes/login role=vault-sync-operator \
  jwt="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)"
```

## Still Having Issues?

If you're still experiencing problems after following this guide:

1. **Check operator logs**: Look for specific error messages
2. **Verify all components**: Ensure Vault, Kubernetes, and the operator are all healthy
3. **Test step-by-step**: Follow the manual authentication steps to isolate the issue
4. **Check network policies**: Ensure there are no network restrictions between the operator and Vault
5. **Verify versions**: Check compatibility between Vault version and Kubernetes version

Create an issue with the following information:
- Exact error messages from operator logs
- Output of `vault read auth/kubernetes/config`
- Output of `vault read auth/kubernetes/role/vault-sync-operator`
- Output of `vault policy read vault-sync-operator`
- Kubernetes version and Vault version