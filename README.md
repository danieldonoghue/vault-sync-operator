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
git clone https://github.com/danield/vault-sync-operator.git
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
    vault-sync.io/enabled: "true"
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
```

This will sync the `username` and `password` keys from the `my-app-secrets` secret to Vault at the path `secret/data/my-app` with the keys prefixed as `app_username` and `app_password`.

### Annotations Reference

| Annotation | Required | Description | Example |
|------------|----------|-------------|---------|
| `vault-sync.io/enabled` | Yes | Enable vault sync for this deployment | `"true"` |
| `vault-sync.io/path` | Yes | Vault path where secrets should be stored | `"secret/data/my-app"` |
| `vault-sync.io/secrets` | Yes | JSON array of secret configurations | See below |

### Secrets Configuration Format

The `vault-sync.io/secrets` annotation expects a JSON array with the following structure:

```json
[
  {
    "name": "secret-name",           // Name of the Kubernetes secret
    "keys": ["key1", "key2"],        // Array of keys to sync from the secret
    "prefix": "optional_prefix_"     // Optional prefix for keys in Vault
  }
]
```

### Advanced Examples

#### Multiple Secrets
```yaml
annotations:
  vault-sync.io/enabled: "true"
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

#### No Prefix
```yaml
annotations:
  vault-sync.io/enabled: "true"
  vault-sync.io/path: "secret/data/simple-app"
  vault-sync.io/secrets: |
    [
      {
        "name": "simple-secrets",
        "keys": ["username", "password"]
      }
    ]
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

### Common Issues

1. **Authentication Failures**:
   - Verify Vault Kubernetes auth backend is properly configured
   - Check service account permissions
   - Ensure the operator's service account is bound to the correct Vault role

2. **Secret Not Found Errors**:
   - Verify the secret exists in the same namespace as the deployment
   - Check that all specified keys exist in the secret

3. **Vault Write Failures**:
   - Check Vault policies allow writing to the specified path
   - Verify Vault is accessible from the cluster

### Logs

Check the operator logs for detailed error messages:

```bash
kubectl logs -n vault-sync-operator-system deployment/vault-sync-operator-controller-manager
```

### Health Checks

The operator exposes health check endpoints:

- Liveness: `http://localhost:8081/healthz`
- Readiness: `http://localhost:8081/readyz`

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests
5. Submit a pull request

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.

## Support

For questions and support:

- Create an issue in this repository
- Check the troubleshooting section above
- Review the logs for error messages
