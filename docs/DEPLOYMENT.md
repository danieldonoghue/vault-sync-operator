# Vault Sync Operator Deployment Guide

This guide covers all available deployment methods for the Vault Sync Operator.

## Deployment Options

The Vault Sync Operator supports three deployment methods:

1. **Helm Chart** (Recommended) - Most flexible and production-ready
2. **Kustomize** - Good for GitOps workflows
3. **Manual kubectl** - Simple direct deployment

## Prerequisites

- Kubernetes cluster (v1.19+)
- kubectl configured
- HashiCorp Vault installed and configured
- Vault Kubernetes authentication enabled

## Method 1: Helm Chart (Recommended)

### Installation

```bash
# Add the repository (if publishing to a Helm repository)
# helm repo add vault-sync-operator https://your-repo.com/charts

# Install from local chart
helm install vault-sync-operator ./charts/vault-sync-operator \
  --namespace vault-sync-operator-system \
  --create-namespace
```

### Configuration

Create a `values.yaml` file to customize the deployment:

```yaml
# values.yaml
vault:
  address: "http://your-vault-server:8200"
  role: "vault-sync-operator"
  authPath: "kubernetes"

image:
  repository: ghcr.io/danieldonoghue/vault-sync-operator
  tag: "v0.0.1-alpha.1"

controllerManager:
  resources:
    limits:
      cpu: 500m
      memory: 128Mi
    requests:
      cpu: 10m
      memory: 64Mi
```

Install with custom values:

```bash
helm install vault-sync-operator ./charts/vault-sync-operator \
  --namespace vault-sync-operator-system \
  --create-namespace \
  --values values.yaml
```

### Upgrade

```bash
helm upgrade vault-sync-operator ./charts/vault-sync-operator \
  --namespace vault-sync-operator-system \
  --values values.yaml
```

### Uninstallation

```bash
helm uninstall vault-sync-operator -n vault-sync-operator-system
kubectl delete namespace vault-sync-operator-system
```

## Method 2: Kustomize

### Installation

```bash
# Deploy using kustomize
kubectl apply -k config/default/
```

### Configuration

**⚠️ Important**: The default configuration assumes Vault is running inside the cluster. For external Vault servers, you must update the VAULT_ADDR.

Edit `config/manager/manager.yaml` to update Vault settings:

```yaml
env:
- name: VAULT_ADDR
  value: "http://192.168.1.100:8200"  # Change to your Vault server address
- name: VAULT_ROLE
  value: "vault-sync-operator"
- name: VAULT_AUTH_PATH
  value: "kubernetes"
```

Edit `config/default/kustomization.yaml` to customize:

```yaml
# config/default/kustomization.yaml
namespace: vault-sync-operator-system
namePrefix: vault-sync-operator-

patches:
- path: manager_auth_proxy_patch.yaml
```

For detailed Vault address configuration, see [VAULT-ADDRESS-CONFIGURATION.md](VAULT-ADDRESS-CONFIGURATION.md).

### Uninstallation

```bash
kubectl delete -k config/default/
```

## Method 3: Manual kubectl

### Installation

```bash
# Apply manifests in order
kubectl apply -f deploy/manual/00-namespace.yaml
kubectl apply -f deploy/manual/01-serviceaccount.yaml
kubectl apply -f deploy/manual/02-rbac.yaml
kubectl apply -f deploy/manual/03-crd.yaml
kubectl apply -f deploy/manual/04-deployment.yaml
kubectl apply -f deploy/manual/05-service.yaml

# Or apply all at once
kubectl apply -f deploy/manual/ --recursive
```

### Configuration

**⚠️ Important**: The default configuration assumes Vault is running inside the cluster. For external Vault servers, you must update the VAULT_ADDR.

Edit `deploy/manual/04-deployment.yaml` to configure Vault settings:

```yaml
env:
- name: VAULT_ADDR
  value: "http://192.168.1.100:8200"  # Change to your Vault server address
- name: VAULT_ROLE
  value: "vault-sync-operator"
- name: VAULT_AUTH_PATH
  value: "kubernetes"
```

For detailed Vault address configuration, see [VAULT-ADDRESS-CONFIGURATION.md](VAULT-ADDRESS-CONFIGURATION.md).

### Uninstallation

```bash
kubectl delete -f deploy/manual/ --recursive
```

## Vault Configuration

### Prerequisites

Before configuring Vault authentication, ensure:
- Vault is accessible from your Kubernetes cluster
- You have admin access to Vault
- The vault-sync-operator is deployed and running

### Kubernetes Authentication Setup

**Step 1: Enable Kubernetes Authentication**

```bash
# Enable the Kubernetes auth backend
vault auth enable kubernetes
```

**Step 2: Configure the Auth Backend**

From within your Kubernetes cluster (recommended approach):

```bash
# Create a temporary pod to configure Vault
kubectl run vault-config --image=vault:1.15.2 --rm -it --restart=Never -- sh

# Inside the pod, configure the auth backend
vault write auth/kubernetes/config \
    kubernetes_host="https://kubernetes.default.svc.cluster.local" \
    kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt \
    token_reviewer_jwt="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)"
```

Alternatively, from outside the cluster:

```bash
# Get cluster information
KUBE_CA_CERT=$(kubectl config view --raw --minify --flatten -o jsonpath='{.clusters[].cluster.certificate-authority-data}' | base64 -d)
KUBE_HOST=$(kubectl config view --raw --minify --flatten -o jsonpath='{.clusters[].cluster.server}')

# Configure using external access
vault write auth/kubernetes/config \
    kubernetes_host="$KUBE_HOST" \
    kubernetes_ca_cert="$KUBE_CA_CERT" \
    token_reviewer_jwt="$(kubectl get secret -n vault-sync-operator-system -o go-template='{{range .items}}{{if eq .type "kubernetes.io/service-account-token"}}{{.data.token}}{{end}}{{end}}' | base64 -d)"
```

**Step 3: Create Policy**

Create a policy that allows the operator to read secrets:

```bash
# For KV v2 engine (default)
vault policy write vault-sync-operator - <<EOF
path "secret/data/*" {
  capabilities = ["read"]
}
path "secret/metadata/*" {
  capabilities = ["read"]
}
EOF

# For KV v1 engine (if using legacy setup)
vault policy write vault-sync-operator - <<EOF
path "secret/*" {
  capabilities = ["read"]
}
EOF
```

**Step 4: Create Role**

Create a role that binds the service account to the policy:

```bash
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator-controller-manager \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=24h
```

**Step 5: Verify Configuration**

```bash
# Check auth backend configuration
vault read auth/kubernetes/config

# Check role configuration
vault read auth/kubernetes/role/vault-sync-operator

# Verify policy
vault policy read vault-sync-operator
```

### Testing Authentication

Test that the operator can authenticate:

```bash
# Get service account token
SA_TOKEN=$(kubectl get secret -n vault-sync-operator-system -o go-template='{{range .items}}{{if eq .type "kubernetes.io/service-account-token"}}{{.data.token}}{{end}}{{end}}' | base64 -d)

# Test authentication
vault write auth/kubernetes/login role=vault-sync-operator jwt="$SA_TOKEN"
```

If authentication fails, see the [Vault Authentication Troubleshooting Guide](VAULT-AUTH-TROUBLESHOOTING.md) for detailed debugging steps.

### Common Issues and Solutions

1. **"permission denied" during authentication**
   - Check service account name matches exactly
   - Verify namespace is correct
   - Ensure auth backend is properly configured

2. **"permission denied" when accessing secrets**
   - Verify policy paths match your KV engine version
   - Check that policy is attached to the role
   - Ensure secrets exist at the specified paths

3. **Connection timeouts**
   - Verify Vault is accessible from the cluster
   - Check network policies and firewall rules
   - Ensure correct Vault address in deployment

For comprehensive troubleshooting, refer to [VAULT-AUTH-TROUBLESHOOTING.md](VAULT-AUTH-TROUBLESHOOTING.md).

## Verification

### Check Deployment

```bash
kubectl get all -n vault-sync-operator-system
```

### Check Logs

```bash
kubectl logs -n vault-sync-operator-system -l control-plane=controller-manager -f
```

### Test with VaultSync Resource

```yaml
apiVersion: vault.example.com/v1alpha1
kind: VaultSync
metadata:
  name: test-sync
  namespace: default
spec:
  vaultPath: "secret/test-app"
  secretName: "test-app-secret"
  refreshInterval: "30s"
```

```bash
kubectl apply -f test-vaultsync.yaml
kubectl get secret test-app-secret -o yaml
```

## Troubleshooting

### Common Issues

1. **Authentication failures**
   - Check Vault role configuration
   - Verify service account permissions
   - Check Vault connectivity

2. **RBAC issues**
   - Verify ClusterRole permissions
   - Check ServiceAccount binding

3. **Image pull issues**
   - Verify image repository access
   - Check imagePullSecrets if using private registry

### Debug Commands

```bash
# Check operator status
kubectl get pods -n vault-sync-operator-system
kubectl describe pod -n vault-sync-operator-system -l control-plane=controller-manager

# Check CRD installation
kubectl get crd vaultsyncs.vault.example.com

# Check RBAC
kubectl auth can-i --list --as=system:serviceaccount:vault-sync-operator-system:vault-sync-operator-controller-manager

# Test Vault connectivity
kubectl run vault-test --image=vault:1.15.2 --rm -it -- vault version
```

## Security Considerations

- Use least privilege RBAC policies
- Enable Pod Security Standards
- Use non-root security contexts
- Rotate service account tokens regularly
- Monitor operator logs for security events

## Production Recommendations

- Use Helm chart for production deployments
- Configure resource limits and requests
- Enable monitoring and alerting
- Use persistent volumes for any stateful components
- Implement proper backup strategies for Vault
- Use network policies to restrict traffic