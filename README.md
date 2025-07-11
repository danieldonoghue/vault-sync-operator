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

The Vault Sync Operator supports multiple deployment methods:

1. **Helm Chart (Recommended)**:
```bash
git clone https://github.com/danieldonoghue/vault-sync-operator.git
cd vault-sync-operator
helm install vault-sync-operator ./charts/vault-sync-operator \
  --namespace vault-sync-operator-system \
  --create-namespace
```

2. **Kustomize**:
```bash
git clone https://github.com/danieldonoghue/vault-sync-operator.git
cd vault-sync-operator
kubectl apply -k config/default/
```

3. **Manual kubectl**:
```bash
git clone https://github.com/danieldonoghue/vault-sync-operator.git
cd vault-sync-operator
kubectl apply -f deploy/manual/ --recursive
```

4. **Make (Development)**:
```bash
git clone https://github.com/danieldonoghue/vault-sync-operator.git
cd vault-sync-operator
make deploy
```

For detailed deployment instructions, see [DEPLOYMENT.md](docs/DEPLOYMENT.md).

### Configuration

1. **Configure Vault Kubernetes Auth Backend**:

For detailed Vault setup instructions, see [VAULT-SETUP-GUIDE.md](docs/VAULT-SETUP-GUIDE.md).

Quick setup:

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

The operator uses the following annotations to control secret synchronization:

| Annotation | Required | Description | Example |
|------------|----------|-------------|---------|
| `vault-sync.io/path` | ✅ | Vault storage path (enables sync) | `"secret/data/my-app"` |
| `vault-sync.io/secrets` | ❌ | Custom secret configuration (JSON) | See examples below |
| `vault-sync.io/preserve-on-delete` | ❌ | Prevent deletion from Vault on deployment deletion | `"true"` |

### Annotation Examples

#### Basic Sync (Auto-Discovery)
```yaml
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    # All secrets referenced in pod template will be synced
```

#### Custom Secret Selection
```yaml
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    vault-sync.io/secrets: |
      [
        {
          "name": "database-secret",
          "keys": ["username", "password"],
          "prefix": "db_"
        },
        {
          "name": "api-secret", 
          "keys": ["token"]
        }
      ]
```

#### Preserve Secrets on Deletion
```yaml
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    vault-sync.io/preserve-on-delete: "true"
    # Secret will NOT be deleted from Vault when deployment is deleted
```

## Multi-Cluster Support

The operator follows the standard Kubernetes pattern of **per-cluster deployment**. Each cluster runs its own operator instance, which is the recommended approach for:

- Security isolation between clusters
- Simple architecture without cross-cluster networking
- Independent cluster operations
- Following Kubernetes operator best practices

### Multi-Cluster Configuration

Deploy the operator in each cluster with cluster-specific configuration:

```bash
# Cluster A
./bin/manager --cluster-name=production-us-east \
              --vault-auth-path=kubernetes-prod-us-east

# Cluster B  
./bin/manager --cluster-name=production-eu-west \
              --vault-auth-path=kubernetes-prod-eu-west
```

Vault paths will be automatically organized by cluster:
- Cluster A: `clusters/production-us-east/secret/data/my-app`
- Cluster B: `clusters/production-eu-west/secret/data/my-app`

See [Multi-Cluster Deployment Guide](docs/multi-cluster-deployment.md) for complete setup instructions.

## Secret Generators Support

The operator works with secret generators (Kustomize, Helm, etc.) as it:

1. **Reads secrets at runtime**: Doesn't watch secret creation, only reads when syncing
2. **Auto-discovers references**: Finds secrets referenced in deployment pod templates
3. **Provides helpful error messages**: Suggests checking if secret generators have run

**Recommendation**: Ensure secret generators run before the operator reconciles deployments.

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

The operator uses the following annotations to control secret synchronization:

| Annotation | Required | Description | Example |
|------------|----------|-------------|---------|
| `vault-sync.io/path` | ✅ | Vault storage path (enables sync) | `"secret/data/my-app"` |
| `vault-sync.io/secrets` | ❌ | Custom secret configuration (JSON) | See examples below |
| `vault-sync.io/preserve-on-delete` | ❌ | Prevent deletion from Vault on deployment deletion | `"true"` |

### Annotation Examples

#### Basic Sync (Auto-Discovery)
```yaml
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    # All secrets referenced in pod template will be synced
```

#### Custom Secret Selection
```yaml
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    vault-sync.io/secrets: |
      [
        {
          "name": "database-secret",
          "keys": ["username", "password"],
          "prefix": "db_"
        },
        {
          "name": "api-secret", 
          "keys": ["token"]
        }
      ]
```

#### Preserve Secrets on Deletion
```yaml
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    vault-sync.io/preserve-on-delete: "true"
    # Secret will NOT be deleted from Vault when deployment is deleted
```

## Multi-Cluster Support

The operator follows the standard Kubernetes pattern of **per-cluster deployment**. Each cluster runs its own operator instance, which is the recommended approach for:

- Security isolation between clusters
- Simple architecture without cross-cluster networking
- Independent cluster operations
- Following Kubernetes operator best practices

### Multi-Cluster Configuration

Deploy the operator in each cluster with cluster-specific configuration:

```bash
# Cluster A
./bin/manager --cluster-name=production-us-east \
              --vault-auth-path=kubernetes-prod-us-east

# Cluster B  
./bin/manager --cluster-name=production-eu-west \
              --vault-auth-path=kubernetes-prod-eu-west
```

Vault paths will be automatically organized by cluster:
- Cluster A: `clusters/production-us-east/secret/data/my-app`
- Cluster B: `clusters/production-eu-west/secret/data/my-app`

See [Multi-Cluster Deployment Guide](docs/multi-cluster-deployment.md) for complete setup instructions.

## Secret Generators Support

The operator works with secret generators (Kustomize, Helm, etc.) as it:

1. **Reads secrets at runtime**: Doesn't watch secret creation, only reads when syncing
2. **Auto-discovers references**: Finds secrets referenced in deployment pod templates
3. **Provides helpful error messages**: Suggests checking if secret generators have run

**Recommendation**: Ensure secret generators run before the operator reconciles deployments.

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

The operator uses the following annotations to control secret synchronization:

| Annotation | Required | Description | Example |
|------------|----------|-------------|---------|
| `vault-sync.io/path` | ✅ | Vault storage path (enables sync) | `"secret/data/my-app"` |
| `vault-sync.io/secrets` | ❌ | Custom secret configuration (JSON) | See examples below |
| `vault-sync.io/preserve-on-delete` | ❌ | Prevent deletion from Vault on deployment deletion | `"true"` |

### Annotation Examples

#### Basic Sync (Auto-Discovery)
```yaml
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    # All secrets referenced in pod template will be synced
```

#### Custom Secret Selection
```yaml
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    vault-sync.io/secrets: |
      [
        {
          "name": "database-secret",
          "keys": ["username", "password"],
          "prefix": "db_"
        },
        {
          "name": "api-secret", 
          "keys": ["token"]
        }
      ]
```

#### Preserve Secrets on Deletion
```yaml
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    vault-sync.io/preserve-on-delete: "true"
    # Secret will NOT be deleted from Vault when deployment is deleted
```

## Multi-Cluster Support

The operator follows the standard Kubernetes pattern of **per-cluster deployment**. Each cluster runs its own operator instance, which is the recommended approach for:

- Security isolation between clusters
- Simple architecture without cross-cluster networking
- Independent cluster operations
- Following Kubernetes operator best practices

### Multi-Cluster Configuration

Deploy the operator in each cluster with cluster-specific configuration:

```bash
# Cluster A
./bin/manager --cluster-name=production-us-east \
              --vault-auth-path=kubernetes-prod-us-east

# Cluster B  
./bin/manager --cluster-name=production-eu-west \
              --vault-auth-path=kubernetes-prod-eu-west
```

Vault paths will be automatically organized by cluster:
- Cluster A: `clusters/production-us-east/secret/data/my-app`
- Cluster B: `clusters/production-eu-west/secret/data/my-app`

See [Multi-Cluster Deployment Guide](docs/multi-cluster-deployment.md) for complete setup instructions.

## Secret Generators Support

The operator works with secret generators (Kustomize, Helm, etc.) as it:

1. **Reads secrets at runtime**: Doesn't watch secret creation, only reads when syncing
2. **Auto-discovers references**: Finds secrets referenced in deployment pod templates
3. **Provides helpful error messages**: Suggests checking if secret generators have run

**Recommendation**: Ensure secret generators run before the operator reconciles deployments.

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

## Container Runtime Optimization

The operator is optimized for Kubernetes container environments with automatic Go runtime configuration:

### Automatic Resource Detection

- **GOMAXPROCS**: Automatically configured based on container CPU limits using `go.uber.org/automaxprocs`
- **GOMEMLIMIT**: Set from container memory limits to optimize garbage collection
- **Container-aware GC**: Prevents memory usage beyond container limits

### Resource Configuration

The default deployment includes:

```yaml
resources:
  limits:
    cpu: 500m      # Automatically sets GOMAXPROCS
    memory: 128Mi  # Automatically sets GOMEMLIMIT
  requests:
    cpu: 10m
    memory: 64Mi
```

### Runtime Validation

The operator logs its runtime configuration at startup:

```
Go runtime configuration GOMAXPROCS=1 NumCPU=4 GOMEMLIMIT=128Mi container_memory_limit=128Mi container_cpu_limit=auto-detected=1
```

### Tuning for Production

For production workloads, adjust resources based on:

- **CPU**: Increase for high-volume secret synchronization
- **Memory**: Increase for deployments with many large secrets
- **Replicas**: Enable leader election for high availability

Example production configuration:

```yaml
resources:
  limits:
    cpu: 1000m     # GOMAXPROCS=1
    memory: 256Mi  # GOMEMLIMIT=256Mi
  requests:
    cpu: 100m
    memory: 128Mi
```
