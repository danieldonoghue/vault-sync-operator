# Vault Sync Operator Examples

This directory contains example configurations demonstrating various features of the Vault Sync Operator.

## Deployment-Based Sync Examples

These examples show how to sync secrets to Vault based on Deployment annotations.

- **[basic-example.yaml](basic-example.yaml)** - Basic deployment with custom secret configuration
- **[auto-discovery-example.yaml](auto-discovery-example.yaml)** - Auto-discovery of secrets from deployment pod template
- **[multiple-secrets-example.yaml](multiple-secrets-example.yaml)** - Multiple secrets with different configurations
- **[performance-optimization-example.yaml](performance-optimization-example.yaml)** - Performance optimized configurations
- **[preserve-secrets-example.yaml](preserve-secrets-example.yaml)** - Preserve secrets in Vault on deletion
- **[periodic-reconciliation-example.yaml](periodic-reconciliation-example.yaml)** - Periodic reconciliation settings
- **[rotation-detection-example.yaml](rotation-detection-example.yaml)** - Secret rotation detection configuration
- **[production-deployment.yaml](production-deployment.yaml)** - Production-ready deployment example
- **[troubleshooting-example.yaml](troubleshooting-example.yaml)** - Common troubleshooting scenarios

## Direct Secret Sync Examples

These examples show how to sync secrets directly to Vault using Secret annotations.

- **[secret-level-sync-example.yaml](secret-level-sync-example.yaml)** - Comprehensive Secret-level sync examples including:
  - Sync all keys from a Secret
  - Sync specific keys with prefixes
  - Preservation and reconciliation settings
  - Multi-environment configurations
  - Rotation check disabled scenarios

## Usage

Apply any example with:

```bash
kubectl apply -f examples/basic-example.yaml
```

## Configuration Patterns

### Deployment-Based Sync
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    # Optional custom configuration:
    vault-sync.io/secrets: |
      [{"name": "my-secret", "keys": ["key1"], "prefix": "app_"}]
```

### Direct Secret Sync
```yaml
apiVersion: v1
kind: Secret
metadata:
  annotations:
    vault-sync.io/path: "secret/data/my-secret"
    # Optional custom configuration:
    vault-sync.io/secrets: |
      [{"name": "my-secret", "keys": ["key1"], "prefix": "prod_"}]
```

## Monitoring

See **[grafana-dashboard.json](grafana-dashboard.json)** for a comprehensive monitoring dashboard that tracks both deployment and secret sync operations.

## Notes

- All examples assume the operator is installed with default configuration
- Adjust Vault paths according to your Vault setup (KV v1 vs v2)
- Ensure proper RBAC permissions are configured for the operator
- Test in development environments before applying to production