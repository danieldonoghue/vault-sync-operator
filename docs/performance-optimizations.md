# Performance Optimizations

The vault-sync-operator includes several performance optimizations to handle high-volume secret synchronization efficiently while minimizing resource usage and Vault server load.

## Rate Limiting

### Overview

The operator implements intelligent rate limiting to prevent overwhelming Vault servers, especially in environments with many deployments or frequent secret updates.

### Configuration

Rate limiting is configured automatically with sensible defaults:

- **Rate**: 10 requests per second
- **Burst**: 20 requests
- **Scope**: Per operator instance

### Implementation

```go
// Rate limiter allows 10 requests/second with burst of 20
rateLimiter := rate.NewLimiter(rate.Limit(10), 20)
```

All Vault operations (read, write, delete) respect the rate limit, ensuring smooth operation even under high load.

## Batch Operations

### Overview

When multiple secrets need to be processed, the operator batches operations to improve efficiency and reduce the total number of Vault API calls.

### Features

1. **Batch Size Control**: Processes secrets in configurable batches (default: 5 operations per batch)
2. **Concurrent Safety**: Thread-safe batch processing with mutex protection
3. **Error Handling**: Individual operation failures don't affect the entire batch
4. **Respect Rate Limits**: Each operation in a batch still respects rate limiting

### Usage

The batch operations are used automatically when multiple secrets are detected in a deployment:

```go
// Example: Multiple secrets from pod template are batched
operations := []BatchOperation{
    {Path: "secret/app1", Data: secret1Data, Type: "write"},
    {Path: "secret/app2", Data: secret2Data, Type: "write"},
    {Path: "secret/app3", Data: secret3Data, Type: "write"},
}

err := vaultClient.BatchWriteSecrets(ctx, operations)
```

### Benefits

- **Reduced API Overhead**: Fewer individual API calls
- **Better Resource Utilization**: More efficient use of network and CPU
- **Improved Throughput**: Higher overall secret processing rate
- **Graceful Backoff**: Built-in delays between batches prevent server overload

## Memory Optimization

### Large Secret Handling

The operator includes optimizations for handling large secrets that might cause memory pressure:

#### Size Detection

```go
func (c *Client) isDataTooLarge(data map[string]interface{}) bool {
    // Considers secrets > 1MB as "large"
    return calculateSize(data) > 1024*1024
}
```

#### Optimization Strategies

1. **Size Monitoring**: Tracks secret sizes and logs warnings for large secrets
2. **Memory-Aware Processing**: Uses specialized handling for large secrets
3. **Efficient Processing**: Optimized architecture for handling large secrets

### Memory Usage Patterns

- **Efficient JSON Marshaling**: Uses optimized JSON serialization for metadata
- **Minimal Buffering**: Processes secrets without unnecessary intermediate copies
- **Resource Version Tracking**: Lightweight version comparison without full secret loading

## Monitoring and Metrics

### Performance Metrics

The operator exposes several metrics to monitor performance:

#### Rate Limiting Metrics
- `vault_rate_limit_wait_duration_seconds`: Time spent waiting for rate limiter
- `vault_rate_limit_requests_total`: Total rate-limited requests

#### Batch Operation Metrics
- `vault_batch_operations_total`: Total batch operations performed
- `vault_batch_size_histogram`: Distribution of batch sizes
- `vault_batch_duration_seconds`: Time taken for batch operations

#### Memory Metrics
- `vault_large_secret_count`: Number of large secrets processed
- `vault_memory_optimization_applied_total`: Times memory optimization was used

### Health Checks

Monitor operator performance using these indicators:

1. **Rate Limit Utilization**: Monitor wait times to ensure rate limits aren't too restrictive
2. **Batch Efficiency**: Track batch sizes and success rates
3. **Memory Usage**: Monitor for memory growth patterns
4. **Error Rates**: Watch for timeout or resource exhaustion errors

## Configuration Tuning

### Performance Configuration

The operator provides built-in performance optimizations and can be tuned through deployment configuration. See the [Deployment Guide](DEPLOYMENT.md) for resource limits, replica scaling, and operational parameters.

### Scaling Considerations

#### Horizontal Scaling

When running multiple operator instances:

- Each instance has its own rate limiter
- Total Vault load = instances × rate_limit
- Consider Vault server capacity when scaling

#### Vertical Scaling

For single instance optimization:

- Increase rate limits if Vault can handle higher load
- Increase batch sizes for better throughput
- Monitor memory usage for large secret workloads

## Best Practices

### Deployment Design

1. **Secret Size**: Keep individual secrets under 1MB when possible
2. **Secret Count**: Limit number of secrets per deployment for optimal batching
3. **Update Frequency**: Use rotation detection to minimize unnecessary syncs

### Vault Configuration

1. **Connection Pooling**: Ensure Vault client uses connection pooling
2. **Server Capacity**: Size Vault servers appropriately for expected load
3. **Monitoring**: Monitor Vault server metrics alongside operator metrics

### Kubernetes Setup

1. **Resource Limits**: Set appropriate CPU/memory limits for the operator
2. **Node Affinity**: Consider placing operator near Vault servers for better latency
3. **Network Policies**: Ensure optimal network paths between operator and Vault

## Troubleshooting

### High Latency

If operations are slow:

1. Check rate limit wait times in metrics
2. Verify network connectivity to Vault
3. Monitor Vault server load and capacity
4. Consider increasing rate limits if Vault can handle it

### Memory Issues

If memory usage is high:

1. Check for large secrets in logs
2. Monitor secret size distribution
3. Consider splitting large secrets into smaller ones
4. Verify no memory leaks in long-running operations

### Vault Overload

If Vault servers are overwhelmed:

1. Reduce rate limits temporarily
2. Increase batch processing delays
3. Scale Vault infrastructure
4. Monitor Vault connectivity and adjust operations accordingly

### Error Recovery

The operator includes robust error handling:

1. **Retry Logic**: Automatic retries with exponential backoff
2. **Error Handling**: Comprehensive error detection and logging
3. **Graceful Degradation**: Continues operating even if some operations fail
4. **Detailed Logging**: Comprehensive error information for debugging

## Performance Monitoring

The operator exposes comprehensive Prometheus metrics for monitoring performance and identifying optimization opportunities. Key metrics include:

- **Sync Operations**: Success/failure rates and duration
- **Rate Limiting**: Request throttling and queue metrics  
- **Memory Usage**: Resource utilization patterns
- **Vault Response Times**: End-to-end operation latency
- **Error Tracking**: Categorized error rates and types

Configure your monitoring system to alert on performance degradation and use the metrics to tune the deployment for optimal performance in your environment.
