# Vault Address Configuration Guide

The vault-sync-operator needs to know how to connect to your Vault server. This guide shows how to configure the Vault address for different deployment scenarios.

## Problem Statement

The default configuration assumes Vault is running inside the cluster with a service named `vault`. However, Vault is often running:
- On a VM outside the cluster
- On a different host/port
- Behind a load balancer
- With a different service name

## Configuration Methods

### Method 1: Helm Chart (Recommended)

The Helm chart makes the Vault address easily configurable:

```yaml
# values.yaml
vault:
  address: "http://192.168.1.100:8200"  # Your Vault server address
  role: "vault-sync-operator"
  authPath: "kubernetes"
```

Deploy with custom address:

```bash
helm install vault-sync-operator ./charts/vault-sync-operator \
  --namespace vault-sync-operator-system \
  --create-namespace \
  --set vault.address="http://192.168.1.100:8200"
```

### Method 2: Kustomize

For kustomize deployments, edit `config/manager/manager.yaml`:

```yaml
env:
- name: VAULT_ADDR
  value: "http://192.168.1.100:8200"  # Change this to your Vault server address
- name: VAULT_ROLE
  value: "vault-sync-operator"
- name: VAULT_AUTH_PATH
  value: "kubernetes"
```

Or use a kustomize patch:

```yaml
# config/manager/vault-address-patch.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: VAULT_ADDR
          value: "http://192.168.1.100:8200"
```

Add to `config/default/kustomization.yaml`:

```yaml
patches:
- path: manager_auth_proxy_patch.yaml
- path: ../manager/vault-address-patch.yaml  # Add this line
```

### Method 3: Manual kubectl

For manual deployments, edit `deploy/manual/03-deployment.yaml`:

```yaml
env:
- name: VAULT_ADDR
  value: "http://192.168.1.100:8200"  # Change this to your Vault server address
- name: VAULT_ROLE
  value: "vault-sync-operator"
- name: VAULT_AUTH_PATH
  value: "kubernetes"
```

### Method 4: Runtime Configuration

You can also patch the deployment after it's deployed:

```bash
# Using kubectl patch
kubectl patch deployment vault-sync-operator-controller-manager \
  -n vault-sync-operator-system \
  --type='merge' \
  -p='{"spec":{"template":{"spec":{"containers":[{"name":"manager","env":[{"name":"VAULT_ADDR","value":"http://192.168.1.100:8200"}]}]}}}}'

# Using kubectl set env
kubectl set env deployment/vault-sync-operator-controller-manager \
  -n vault-sync-operator-system \
  VAULT_ADDR=http://192.168.1.100:8200
```

## Common Vault Address Patterns

### VM/External Host
```bash
# VM with IP address
VAULT_ADDR="http://192.168.1.100:8200"

# VM with hostname
VAULT_ADDR="http://vault.example.com:8200"

# With custom port
VAULT_ADDR="http://192.168.1.100:8201"
```

### HTTPS/TLS
```bash
# HTTPS with valid certificate
VAULT_ADDR="https://vault.example.com:8200"

# HTTPS with custom port
VAULT_ADDR="https://192.168.1.100:8201"
```

### Kubernetes Service (Internal)
```bash
# Service in same namespace
VAULT_ADDR="http://vault:8200"

# Service in different namespace
VAULT_ADDR="http://vault.vault-system.svc.cluster.local:8200"

# Service with custom name
VAULT_ADDR="http://hashicorp-vault:8200"
```

### Load Balancer
```bash
# Behind load balancer
VAULT_ADDR="http://vault-lb.example.com:8200"

# Cloud load balancer
VAULT_ADDR="https://vault-12345.elb.amazonaws.com:8200"
```

## Verification

### Test Connectivity

After configuring the address, test connectivity:

```bash
# Test from the operator pod
kubectl exec -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager -- \
  curl -s $VAULT_ADDR/v1/sys/health

# Test from a debug pod
kubectl run debug --image=curlimages/curl --rm -it --restart=Never -- \
  curl -s http://192.168.1.100:8200/v1/sys/health
```

### Check Operator Logs

```bash
# Check for connection errors
kubectl logs -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager | grep -i vault

# Look for specific error patterns
kubectl logs -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager | grep -E "(connection|refused|timeout|unreachable)"
```

## Troubleshooting

### Connection Refused
```bash
# Check if port is open
kubectl run netcat --image=busybox --rm -it --restart=Never -- \
  nc -zv 192.168.1.100 8200

# Check DNS resolution
kubectl run dns-test --image=busybox --rm -it --restart=Never -- \
  nslookup vault.example.com
```

### Timeout Issues
```bash
# Check network policies
kubectl get networkpolicies -A

# Check firewall rules on VM
sudo ufw status

# Check iptables rules
sudo iptables -L
```

### TLS Certificate Issues
```bash
# Test TLS connection
kubectl run tls-test --image=curlimages/curl --rm -it --restart=Never -- \
  curl -v https://vault.example.com:8200/v1/sys/health

# Skip TLS verification for testing (not recommended for production)
kubectl run tls-test --image=curlimages/curl --rm -it --restart=Never -- \
  curl -k https://vault.example.com:8200/v1/sys/health
```

## Environment-Specific Examples

### k3s VM Setup
```bash
# Vault running on host machine (VM)
VAULT_ADDR="http://192.168.1.100:8200"

# Vault running on same VM as k3s
VAULT_ADDR="http://localhost:8200"

# Vault running on docker on the VM
VAULT_ADDR="http://172.17.0.1:8200"
```

### Development Environment
```bash
# Vault dev server on host
VAULT_ADDR="http://host.docker.internal:8200"

# Port forwarded Vault
VAULT_ADDR="http://localhost:8200"
```

### Production Environment
```bash
# Production Vault with TLS
VAULT_ADDR="https://vault.prod.example.com:8200"

# Vault behind load balancer
VAULT_ADDR="https://vault-api.example.com"
```

## Configuration Templates

### Helm Values Template
```yaml
# values-production.yaml
vault:
  address: "https://vault.prod.example.com:8200"
  role: "vault-sync-operator"
  authPath: "kubernetes"

image:
  repository: ghcr.io/danieldonoghue/vault-sync-operator
  tag: "latest"
  pullPolicy: IfNotPresent

controllerManager:
  resources:
    limits:
      cpu: 500m
      memory: 128Mi
    requests:
      cpu: 10m
      memory: 64Mi
```

### Kustomize Patch Template
```yaml
# vault-config-patch.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: controller-manager
  namespace: system
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: VAULT_ADDR
          value: "https://vault.prod.example.com:8200"
        - name: VAULT_ROLE
          value: "vault-sync-operator"
        - name: VAULT_AUTH_PATH
          value: "kubernetes"
```

## Security Considerations

### TLS Configuration
- Always use HTTPS in production
- Verify TLS certificates
- Use proper CA certificates

### Network Security
- Restrict access to Vault port (8200/8201)
- Use network policies to limit traffic
- Consider VPN or private networks

### Authentication
- Use appropriate Vault roles
- Limit token TTL
- Monitor authentication attempts

## Best Practices

1. **Use Helm for Production**: Most flexible and maintainable
2. **Environment-Specific Configuration**: Different addresses for dev/staging/prod
3. **Health Checks**: Always verify connectivity after configuration
4. **Monitoring**: Set up alerts for connection failures
5. **Documentation**: Document your specific Vault address for your team

## Quick Reference

Common kubectl commands for Vault address configuration:

```bash
# Get current Vault address
kubectl get deployment vault-sync-operator-controller-manager -n vault-sync-operator-system -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="VAULT_ADDR")].value}'

# Update Vault address
kubectl set env deployment/vault-sync-operator-controller-manager -n vault-sync-operator-system VAULT_ADDR=http://192.168.1.100:8200

# Restart deployment after configuration change
kubectl rollout restart deployment/vault-sync-operator-controller-manager -n vault-sync-operator-system

# Check deployment status
kubectl rollout status deployment/vault-sync-operator-controller-manager -n vault-sync-operator-system
```