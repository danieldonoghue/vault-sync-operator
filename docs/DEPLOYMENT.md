# Vault Sync Operator Deployment Guide

This guide covers all available deployment methods for the Vault Sync Operator.

## Deployment Options

The Vault Sync Operator supports four deployment methods:

1. **Helm Chart** (Recommended) - Most flexible and production-ready
2. **Kustomize** - Good for GitOps workflows
3. **Manual kubectl** - Simple direct deployment
4. **Source-based** - Build and deploy from source code

## Prerequisites

- Kubernetes cluster (v1.19+)
- kubectl configured
- HashiCorp Vault installed and running
- Vault CLI installed and accessible

## Vault Setup

Before deploying the operator, configure Vault for Kubernetes authentication:

```bash
# Set your Vault address and token
export VAULT_ADDR="http://your-vault-server:8200"
export VAULT_TOKEN="your-vault-token"

# Run the setup script to configure Vault
./scripts/setup-vault.sh
```

This script will:
- Enable Kubernetes authentication backend
- Create the necessary policies
- Configure the authentication role
- Set up proper permissions for the operator

For detailed Vault configuration options, see the [Vault Setup Guide](VAULT-SETUP-GUIDE.md).

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

You can customize the deployment in several ways:

**Option 1: Edit the default values file directly**
```bash
# Edit the existing values file
nano ./charts/vault-sync-operator/values.yaml
```

**Option 2: Create a custom values file**

```yaml
# values.yaml
vault:
  address: "http://your-vault-server:8200"
  role: "vault-sync-operator"
  authPath: "kubernetes"

image:
  repository: ghcr.io/danieldonoghue/vault-sync-operator
  tag: "v0.0.1-alpha.3"

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
kubectl apply -f deploy/manual/03-deployment.yaml
kubectl apply -f deploy/manual/04-service.yaml

# Or apply all at once
kubectl apply -f deploy/manual/ --recursive
```

### Configuration

**⚠️ Important**: The default configuration assumes Vault is running inside the cluster. For external Vault servers, you must update the VAULT_ADDR.

Edit `deploy/manual/03-deployment.yaml` to configure Vault settings:

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

## Method 4: Source-based Deployment

This method is ideal for contributors, development environments, or when you need to build from the latest source code.

### Prerequisites

- Go 1.22+ installed
- Docker installed
- kubectl configured
- Git repository cloned

### Local Development

Build and run the operator locally on your development machine:

```bash
# Build the operator binary
make build

# Run locally (requires kubeconfig configured)
# This runs the operator outside the cluster but manages cluster resources
make run
```

### Container Deployment from Source

Build a custom container image and deploy to Kubernetes:

```bash
# Build container image (uses default tag: vault-sync-operator:latest)
make docker-build

# Optional: Build with custom tag
IMG=my-registry/vault-sync-operator:v1.0.0 make docker-build

# Deploy to Kubernetes using kustomize
make deploy

# Optional: Deploy with custom image
IMG=my-registry/vault-sync-operator:v1.0.0 make deploy
```

### Configuration

When using source-based deployment, the default configuration assumes:
- Vault address: `http://vault:8200` (in-cluster)
- Namespace: `vault-sync-operator-system`
- Image: `vault-sync-operator:latest`

To customize these settings, you can:

1. **Set environment variables before deploying:**
```bash
export IMG=my-registry/vault-sync-operator:custom-tag
make deploy
```

2. **Edit kustomization files directly:**
```bash
# Edit the manager configuration
vim config/manager/manager.yaml

# Edit the default kustomization
vim config/default/kustomization.yaml
```

### Uninstallation

```bash
make undeploy
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

Create a policy that allows the operator to create, read, update and delete secrets:

```bash
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

# For KV v1 engine (if using legacy setup)
vault policy write vault-sync-operator - <<EOF
path "secret/*" {
  capabilities = ["create", "update", "delete", "read"]
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

### Test with Annotation-based Deployment

```yaml
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
        - name: SECRET_VALUE
          valueFrom:
            secretKeyRef:
              name: app-secret
              key: password
---
apiVersion: v1
kind: Secret
metadata:
  name: app-secret
  namespace: default
type: Opaque
data:
  password: dGVzdC1wYXNzd29yZA==  # base64: test-password
```

```bash
# Apply the test deployment
kubectl apply -f test-deployment.yaml

# Check if deployment has vault-sync annotations
kubectl get deployment test-app -o yaml | grep "vault-sync.io"

# Check operator logs for sync activity
kubectl logs -n vault-sync-operator-system -l control-plane=controller-manager

# Verify secret was synced to Vault
vault kv get secret/data/test-app/app-secret
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