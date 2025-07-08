# Secret Rotation Detection

The vault-sync-operator includes intelligent secret rotation detection to optimize performance and reduce unnecessary Vault operations. This feature tracks Kubernetes Secret resource versions and only syncs to Vault when actual changes occur.

## How It Works

The operator automatically tracks the `resourceVersion` of each Kubernetes Secret it syncs to Vault. When a reconciliation occurs, it compares the current secret versions with the last known versions stored in the deployment's annotations. Only when changes are detected will the operator perform a sync to Vault.

## Configuration

### Annotations

The operator uses the following annotations to control rotation detection:

#### `vault-sync.io/rotation-check`

Controls how the operator handles secret rotation detection:

- **`enabled`** (default): Normal rotation detection is active
- **`disabled`**: Rotation detection is disabled, operator will always sync
- **Future**: `<frequency>` (e.g., `5m`, `1h`) for periodic sync regardless of changes (planned feature)

#### `vault-sync.io/secret-versions`

This annotation is automatically managed by the operator and stores the last known resource versions of synced secrets. Do not modify this annotation manually.

### Examples

#### Basic Usage (Default Behavior)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    # rotation-check: enabled is the default
spec:
  # ... deployment spec
```

#### Disable Rotation Detection
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
  annotations:
    vault-sync.io/path: "secret/data/my-app"
    vault-sync.io/rotation-check: "disabled"
spec:
  # ... deployment spec
```

When rotation detection is disabled, the operator will sync to Vault on every reconciliation, regardless of whether secrets have changed.

## Performance Benefits

1. **Reduced Vault Load**: Only syncs when secrets actually change
2. **Faster Reconciliation**: Skips expensive Vault operations when no changes are detected
3. **Lower Network Traffic**: Reduces unnecessary API calls to Vault
4. **Better Resource Utilization**: Operator uses less CPU and memory for unchanged deployments

## Monitoring

The operator provides metrics to monitor rotation detection:

- `vault_sync_rotation_checks_total`: Total number of rotation checks performed
- `vault_sync_rotation_detected_total`: Number of times rotation was detected
- `vault_sync_rotation_skipped_total`: Number of times sync was skipped due to no changes

## Troubleshooting

### Force Sync

If you need to force a sync regardless of detected changes:

1. **Temporary**: Set `vault-sync.io/rotation-check: "disabled"` temporarily
2. **One-time**: Delete the `vault-sync.io/secret-versions` annotation to trigger a fresh sync
3. **Restart**: Restart the deployment to trigger a new reconciliation

### Debug Information

The operator logs detailed information about rotation detection:

```
INFO secret rotation detected, syncing to vault
{"changed_secrets": ["my-secret", "another-secret"]}
```

```
INFO no secret changes detected, skipping vault sync
{"last_versions": {"my-secret": "123"}, "current_versions": {"my-secret": "123"}}
```

### Common Issues

1. **Annotation Corruption**: If the `secret-versions` annotation becomes corrupted, delete it to reset
2. **Missing Changes**: If legitimate changes aren't being detected, check that the secret's `resourceVersion` is actually changing
3. **Performance Issues**: If too many unnecessary syncs occur, ensure rotation detection is enabled

## Implementation Details

### Version Tracking

The operator stores secret versions in JSON format in the deployment annotation:

```json
{
  "my-secret": "12345",
  "another-secret": "67890"
}
```

### Change Detection Algorithm

1. Compare current secret `resourceVersion` with stored version
2. Detect new secrets (not in stored versions)
3. Detect removed secrets (in stored versions but not current)
4. Return `true` if any changes detected, `false` otherwise

### Memory Optimization

For large deployments with many secrets, the operator:

- Uses efficient JSON serialization for version storage
- Performs version comparisons in-memory without additional API calls
- Batches Vault operations when changes are detected

## Security Considerations

- Secret versions are stored as metadata only, no secret data is exposed
- Resource versions are Kubernetes-internal identifiers, not sensitive information
- Rotation detection respects existing RBAC and security policies
