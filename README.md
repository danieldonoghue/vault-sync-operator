# Vault Sync Operator

A Kubernetes operator that automatically syncs secrets from Kubernetes to HashiCorp Vault using annotations on Deployments.

## Overview

The Vault Sync Operator watches for Kubernetes Deployments with specific annotations and automatically pushes the referenced secrets to HashiCorp Vault. It uses Vault's Kubernetes authentication backend for secure authentication.

## Features

- **Automatic Secret Synchronization**: Sync Kubernetes secrets to Vault based on deployment annotations
- **Kubernetes Authentication**: Uses Vault's Kubernetes auth backend for secure authentication
- **Selective Secret Keys**: Choose specific keys from secrets to sync to Vault
- **Key Prefixing**: Add prefixes to secret keys when storing in Vault
- **Cleanup on Deletion**: Automatically removes secrets from Vault when deployments are deleted
- **RBAC Support**: Proper Kubernetes RBAC permissions for secure operation

## Quick Start

### Prerequisites

- Kubernetes cluster (v1.20+)
- HashiCorp Vault server with Kubernetes auth backend configured
- `kubectl` configured to access your cluster

### Installation

1. Clone the repository:
```bash
git clone https://github.com/danieldonoghue/vault-sync-operator.git
cd vault-sync-operator
```

2. Build and deploy the operator:
```bash
make deploy
```

### Configuration

1. **Configure Vault Kubernetes Auth Backend**:

```bash
# Enable Kubernetes auth
vault auth enable kubernetes

# Configure the auth backend
vault write auth/kubernetes/config \
    token_reviewer_jwt="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
    kubernetes_host="https://$KUBERNETES_PORT_443_TCP_ADDR:443" \
    kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt

# Create a policy for the operator
vault policy write vault-sync-operator - <<EOF
path "secret/data/*" {
  capabilities = ["create", "update", "delete"]
}
EOF

# Create a role
vault write auth/kubernetes/role/vault-sync-operator \
    bound_service_account_names=vault-sync-operator \
    bound_service_account_namespaces=vault-sync-operator-system \
    policies=vault-sync-operator \
    ttl=24h
```

## Monitoring and Observability

The Vault Sync Operator provides comprehensive monitoring and error handling capabilities to ensure reliable operation.

### Health and Readiness Checks

The operator exposes standard Kubernetes health and readiness endpoints:

- **Health Check** (`/healthz`): Validates connectivity to Vault server
- **Readiness Check** (`/readyz`): Ensures Vault authentication is working correctly

These endpoints are automatically configured and can be used by Kubernetes for container health monitoring.

### Prometheus Metrics

The operator exposes Prometheus metrics on port `:8080` by default. Available metrics include:

#### Sync Operation Metrics
- `vault_sync_operator_sync_attempts_total`: Total number of secret sync attempts (labeled by namespace, deployment, result)
- `vault_sync_operator_sync_duration_seconds`: Duration of secret sync operations in seconds
- `vault_sync_operator_secrets_discovered`: Number of secrets auto-discovered in deployments

#### Error Metrics
- `vault_sync_operator_secret_not_found_errors_total`: Kubernetes secrets that couldn't be found
- `vault_sync_operator_secret_key_missing_errors_total`: Missing keys within secrets
- `vault_sync_operator_config_parse_errors_total`: Configuration parsing errors
- `vault_sync_operator_vault_write_errors_total`: Vault write errors (categorized by error type)

#### Authentication Metrics
- `vault_sync_operator_auth_attempts_total`: Vault authentication attempts and results

### Error Handling and Logging

The operator provides detailed error reporting for common failure scenarios:

#### Secret-Related Errors
- **Secret Not Found**: When a referenced Kubernetes secret doesn't exist
- **Key Not Found**: When a specified key doesn't exist within a secret
- **Available Keys Reporting**: Error logs include available keys to aid troubleshooting

#### Vault-Related Errors
- **Authentication Failures**: When Vault authentication fails or tokens expire
- **Permission Denied**: When the operator lacks write permissions to the specified Vault path
- **Invalid Path**: When the Vault path doesn't exist or is malformed
- **Connection Issues**: Network connectivity problems with Vault

#### Configuration Errors
- **JSON Parse Errors**: When the `vault-sync.io/secrets` annotation contains invalid JSON
- **Invalid Annotation Format**: When required annotations are malformed

All errors are logged with structured logging including relevant context (namespace, deployment, secret names, etc.) and are tracked via Prometheus metrics for alerting and monitoring.

## Usage

### Basic Example

1. **Create a Secret**:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-app-secrets
  namespace: default
type: Opaque
data:
  username: YWRtaW4=  # base64 encoded "admin"
  password: MWYyZDFlMmU2N2Rm  # base64 encoded "1f2d1e2e67df"
```

2. **Create a Deployment with Vault Sync Annotations**:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  namespace: default
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    vault-sync.io/secrets: |
      [
        {
          "name": "my-app-secrets",
          "keys": ["username", "password"],
          "prefix": "app_"
        }
      ]
spec:
  replicas: 1
  selector:
    matchLabels:
      app: my-app
  template:
    metadata:
      labels:
        app: my-app
    spec:
      containers:
      - name: my-app
        image: nginx:latest
        ports:
        - containerPort: 80
        env:
        - name: USERNAME
          valueFrom:
            secretKeyRef:
              name: my-app-secrets
              key: username
        - name: PASSWORD
          valueFrom:
            secretKeyRef:
              name: my-app-secrets
              key: password
```

This will sync the `username` and `password` keys from the `my-app-secrets` secret to Vault at the path `secret/data/my-app` with the keys prefixed as `app_username` and `app_password`.

### Annotations Reference

| Annotation | Required | Description | Example |
|------------|----------|-------------|---------|
| `vault-sync.io/path` | Yes | Vault path where secrets should be stored | `"secret/data/my-app"` |
| `vault-sync.io/secrets` | No | JSON array of secret configurations (see below) | See below |

**Note**: The presence of `vault-sync.io/path` automatically enables vault sync for the deployment.

### Secrets Configuration Format

The `vault-sync.io/secrets` annotation is **optional** and allows you to selectively sync specific secrets and keys. If not specified, **all secrets referenced by the deployment will be synced entirely** to the Vault path.

#### When to use `vault-sync.io/secrets`:
- **Selective key syncing**: Only sync specific keys from a secret
- **Key prefixing**: Add prefixes to avoid naming conflicts
- **Multiple secrets**: Combine keys from different secrets
- **Fine-grained control**: Precisely control what gets synced

#### Default behavior (no `vault-sync.io/secrets` annotation):
If you don't specify the `vault-sync.io/secrets` annotation, the operator will:
1. Find all secrets referenced in the deployment's pod template
2. Sync each secret entirely (all keys) to Vault
3. Store them as nested objects under the specified path
4. Use the original secret names as object keys

#### Custom configuration format:
When you do specify `vault-sync.io/secrets`, it expects a JSON array with this structure:

```json
[
  {
    "name": "secret-name",           // Name of the Kubernetes secret
    "keys": ["key1", "key2"],        // Array of keys to sync from the secret
    "prefix": "optional_prefix_"     // Optional prefix for keys in Vault
  }
]
```

#### Storage Format in Vault:
Secrets are stored as **structured objects** in Vault, preserving the key-value structure. For example:
- **Kubernetes secret**: `{"username": "admin", "password": "secret123"}`
- **Stored in Vault**: Same structure at the specified path
- **With prefix**: `{"app_username": "admin", "app_password": "secret123"}`

## Design Philosophy

### Simplicity First
- **Implicit enablement**: Just add a `vault-sync.io/path` annotation
- **Auto-discovery**: The operator automatically finds secrets referenced in your deployment
- **Sensible defaults**: Without configuration, all referenced secrets are synced entirely

### Flexible Configuration
- **Optional fine-tuning**: Use `vault-sync.io/secrets` only when you need selective syncing or prefixing
- **Key selection**: Choose specific keys from secrets when needed
- **Namespace organization**: Use prefixes to avoid key conflicts

### Vault Storage Strategy
The operator stores secrets as **structured JSON objects** in Vault rather than individual key-value pairs because:
- **Preserves structure**: Maintains the original secret organization
- **Easier management**: One Vault path per deployment, not per secret key
- **Better security**: Atomic updates and consistent access patterns
- **Intuitive**: What you put in Kubernetes is what you get in Vault (unless customized)

### Advanced Examples

#### Automatic Secret Detection (No configuration needed)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: simple-app
  namespace: default
  annotations:
    vault-sync.io/path: "secret/data/simple-app"
    # No vault-sync.io/secrets annotation = sync all referenced secrets entirely
spec:
  template:
    spec:
      containers:
      - name: app
        image: nginx:latest
        env:
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: database-creds  # This secret will be auto-synced entirely
              key: password
        - name: API_KEY
          valueFrom:
            secretKeyRef:
              name: api-secrets     # This secret will also be auto-synced entirely
              key: key
```

#### Multiple Secrets with Custom Configuration
```yaml
annotations:
  vault-sync.io/path: "secret/data/my-complex-app"
  vault-sync.io/secrets: |
    [
      {
        "name": "database-secrets",
        "keys": ["host", "port", "username", "password"],
        "prefix": "db_"
      },
      {
        "name": "api-keys",
        "keys": ["api_key", "secret_key"]
      }
    ]
```

#### Mixed Approach (Some secrets auto-detected, some configured)
```yaml
annotations:
  vault-sync.io/path: "secret/data/mixed-app"
  vault-sync.io/secrets: |
    [
      {
        "name": "sensitive-secrets",
        "keys": ["admin_password"],  # Only sync specific keys
        "prefix": "admin_"
      }
    ]
    # Other secrets referenced in the deployment will be auto-synced entirely
```

## Development

### Prerequisites

- Go 1.22+
- Docker
- kubectl
- A Kubernetes cluster for testing

### Building

```bash
# Build the binary
make build

# Build the Docker image
make docker-build

# Run tests
make test
```

### Local Development

1. Install CRDs:
```bash
make install
```

2. Run the operator locally:
```bash
make run
```

### Deployment

```bash
# Deploy to cluster
make deploy

# Undeploy
make undeploy
```

## Configuration Options

The operator supports the following command-line flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--vault-addr` | `http://vault:8200` | Vault server address |
| `--vault-role` | `vault-sync-operator` | Vault Kubernetes auth role |
| `--vault-auth-path` | `kubernetes` | Vault Kubernetes auth path |
| `--metrics-bind-address` | `:8080` | Address for metrics endpoint |
| `--health-probe-bind-address` | `:8081` | Address for health probe endpoint |
| `--leader-elect` | `false` | Enable leader election |

## Security Considerations

1. **RBAC**: The operator only requires read access to Deployments and Secrets in the namespaces it operates in.

2. **Vault Authentication**: Uses Kubernetes service account tokens for authentication with Vault.

3. **Finalizers**: Uses finalizers to ensure cleanup of Vault secrets when deployments are deleted.

4. **Least Privilege**: Configure Vault policies to give the operator only the minimum required permissions.

## Troubleshooting

### Common Error Scenarios

The operator provides detailed error messages and metrics to help diagnose issues. Here are common problems and their solutions:

#### 1. Secret Not Found Errors

**Error**: `failed to get secret mysecret: secrets "mysecret" not found`

**Cause**: The Kubernetes secret referenced in the deployment doesn't exist.

**Solution**: 
- Check if the secret exists: `kubectl get secret mysecret -n <namespace>`
- Create the missing secret or fix the reference in your deployment annotations

**Metrics**: Tracked in `vault_sync_operator_secret_not_found_errors_total`

#### 2. Missing Key Errors

**Error**: `key mykey not found in secret mysecret`

**Cause**: The specified key doesn't exist in the secret.

**Solution**: 
- Check available keys: `kubectl get secret mysecret -o yaml`
- The error log will include available keys to help identify the correct key name
- Update the annotation to use the correct key name

**Metrics**: Tracked in `vault_sync_operator_secret_key_missing_errors_total`

#### 3. Vault Authentication Errors

**Error**: `failed to authenticate with vault: permission denied`

**Cause**: The operator can't authenticate with Vault.

**Solution**:
- Verify Vault's Kubernetes auth backend is configured correctly
- Check that the service account has the correct role assigned
- Ensure the JWT token is valid and accessible

**Metrics**: Tracked in `vault_sync_operator_auth_attempts_total{result="failed"}`

#### 4. Vault Write Permission Errors

**Error**: `failed to write secret to vault: permission denied`

**Cause**: The authenticated role lacks write permissions to the specified path.

**Solution**:
- Update the Vault policy to allow write access to the path
- Verify the role-policy binding in Vault

**Metrics**: Tracked in `vault_sync_operator_vault_write_errors_total{error_type="permission_denied"}`

#### 5. Configuration Parse Errors

**Error**: `failed to parse secrets annotation: invalid character`

**Cause**: The JSON in the `vault-sync.io/secrets` annotation is malformed.

**Solution**:
- Validate the JSON syntax in your annotation
- Use tools like `jq` to validate: `echo '<annotation-value>' | jq .`

**Metrics**: Tracked in `vault_sync_operator_config_parse_errors_total{error_type="json_parse_error"}`

### Debugging with kubectl

1. **Check operator logs**:
```bash
kubectl logs -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager
```

2. **Monitor metrics** (if Prometheus is available):
```bash
# Check sync attempts
kubectl port-forward -n vault-sync-operator-system svc/vault-sync-operator-controller-manager-metrics-service 8080:8443
curl http://localhost:8080/metrics | grep vault_sync_operator
```

3. **Verify deployment annotations**:
```bash
kubectl get deployment <deployment-name> -o yaml | grep -A 10 annotations
```

4. **Test with troubleshooting example**:
```bash
kubectl apply -f examples/troubleshooting-example.yaml
# Check logs for expected error messages
kubectl logs -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager | grep troubleshooting-example
```

### Health Check Endpoints

You can manually check the operator's health:

```bash
# Port forward to access health endpoints
kubectl port-forward -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager 8081:8081

# Check health (basic Vault connectivity)
curl http://localhost:8081/healthz

# Check readiness (full authentication verification)
curl http://localhost:8081/readyz
```
