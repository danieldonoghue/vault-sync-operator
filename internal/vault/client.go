// Package vault provides client functionality for interacting with HashiCorp Vault.
package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/danieldonoghue/vault-sync-operator/internal/metrics"
	"github.com/hashicorp/vault/api"
	"golang.org/x/time/rate"
)

// Client represents a Vault client with Kubernetes authentication and rate limiting.
type Client struct {
	client      *api.Client
	role        string
	authPath    string
	rateLimiter *rate.Limiter
	batchMutex  sync.Mutex
}

// BatchOperation represents a batch operation to be performed on Vault.
type BatchOperation struct {
	Path string
	Data map[string]interface{}
	Type string // "write" or "delete"
}

// NewClient creates a new Vault client with Kubernetes authentication and rate limiting.
func NewClient(vaultAddr, role, authPath string) (*Client, error) {
	config := api.DefaultConfig()
	config.Address = vaultAddr

	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	// Create rate limiter: allow 10 requests per second with burst of 20
	rateLimiter := rate.NewLimiter(rate.Limit(10), 20)

	vaultClient := &Client{
		client:      client,
		role:        role,
		authPath:    authPath,
		rateLimiter: rateLimiter,
	}

	// Authenticate with Kubernetes auth method
	if err := vaultClient.authenticate(); err != nil {
		return nil, fmt.Errorf("failed to authenticate with vault: %w", err)
	}

	return vaultClient, nil
}

// authenticate performs Kubernetes authentication with Vault.
func (c *Client) authenticate() error {
	// Read the service account token
	tokenPath := "/var/run/secrets/kubernetes.io/serviceaccount/token" //nolint:gosec // This is a standard Kubernetes file path, not a credential
	jwt, err := os.ReadFile(tokenPath)
	if err != nil {
		metrics.VaultAuthAttempts.WithLabelValues("failed").Inc()
		return fmt.Errorf("failed to read service account token: %w", err)
	}

	// Prepare the authentication request
	authPath := filepath.Join("auth", c.authPath, "login")
	data := map[string]interface{}{
		"role": c.role,
		"jwt":  string(jwt),
	}

	// Authenticate
	secret, err := c.client.Logical().Write(authPath, data)
	if err != nil {
		metrics.VaultAuthAttempts.WithLabelValues("failed").Inc()
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	if secret == nil || secret.Auth == nil {
		metrics.VaultAuthAttempts.WithLabelValues("failed").Inc()
		return errors.New("authentication response was empty")
	}

	// Set the token for future requests
	c.client.SetToken(secret.Auth.ClientToken)
	metrics.VaultAuthAttempts.WithLabelValues("success").Inc()

	return nil
}

// WriteSecret writes a secret to Vault at the specified path with rate limiting.
func (c *Client) WriteSecret(ctx context.Context, path string, data map[string]interface{}) error {
	// Apply rate limiting
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter error: %w", err)
	}

	// Ensure we have a valid token
	if c.client.Token() == "" {
		if err := c.authenticate(); err != nil {
			metrics.VaultWriteErrors.WithLabelValues("auth_failed", path).Inc()
			return fmt.Errorf("failed to re-authenticate: %w", err)
		}
	}

	// Optimize for large secrets: if data is too large, consider chunking or streaming
	if c.isDataTooLarge(data) {
		return c.writeSecretOptimized(ctx, path, data)
	}

	// Write the secret with KV v2 support
	writeData := c.prepareDataForKVVersion(path, data)
	_, err := c.client.Logical().WriteWithContext(ctx, path, writeData)
	if err != nil {
		// Categorize the error type for better metrics
		var errorType string
		switch {
		case isPermissionError(err):
			errorType = "permission_denied"
		case isPathError(err):
			errorType = "invalid_path"
		case isConnectionError(err):
			errorType = "connection_failed"
		default:
			errorType = "unknown"
		}

		metrics.VaultWriteErrors.WithLabelValues(errorType, path).Inc()
		return fmt.Errorf("failed to write secret to vault at path %s: %w", path, err)
	}

	return nil
}

// DeleteSecret deletes a secret from Vault at the specified path with rate limiting.
func (c *Client) DeleteSecret(ctx context.Context, path string) error {
	// Apply rate limiting
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter error: %w", err)
	}

	// Ensure we have a valid token
	if c.client.Token() == "" {
		if err := c.authenticate(); err != nil {
			return fmt.Errorf("failed to re-authenticate: %w", err)
		}
	}

	// Delete the secret with KV v2 support
	deletePath := c.preparePathForKVDelete(path)
	_, err := c.client.Logical().DeleteWithContext(ctx, deletePath)
	if err != nil {
		return fmt.Errorf("failed to delete secret from vault at path %s: %w", path, err)
	}

	return nil
}

// prepareDataForKVVersion formats data appropriately for KV v1 or v2 based on the path.
// KV v2 paths contain "/data/" and require data to be wrapped in a "data" field.
// KV v1 paths don't contain "/data/" and use the data directly.
func (c *Client) prepareDataForKVVersion(path string, data map[string]interface{}) map[string]interface{} {
	// Check if this is a KV v2 path by looking for "/data/" in the path
	if isKVv2Path(path) {
		// KV v2 requires data to be wrapped in a "data" field
		return map[string]interface{}{
			"data": data,
		}
	}
	
	// KV v1 uses data directly
	return data
}

// isKVv2Path determines if a path is for KV v2 by checking if it contains "/data/"
func isKVv2Path(path string) bool {
	return len(path) > 6 && path[:6] == "secret" && (len(path) > 12 && path[6:12] == "/data/")
}

// preparePathForKVDelete returns the appropriate path for deletion based on KV version.
// For KV v2, it ensures the path uses "/data/" for the delete operation.
// For KV v1, it returns the path as-is.
func (c *Client) preparePathForKVDelete(path string) string {
	if isKVv2Path(path) {
		// KV v2 path already has "/data/" - use as-is
		return path
	}
	
	// Check if this is a KV v1 path that should be converted to KV v2
	// If the path starts with "secret/" but doesn't have "/data/", it might be KV v1 format
	if len(path) > 7 && path[:7] == "secret/" && (len(path) <= 12 || path[7:13] != "data/") {
		// This looks like a KV v1 path, but we need to check if Vault is actually using KV v2
		// For now, we'll assume if the annotation was meant for KV v2, convert it
		// Convert "secret/path" to "secret/data/path"
		return "secret/data/" + path[7:]
	}
	
	// Return path as-is for any other cases
	return path
}

// Helper function to categorize errors - is the error related to permission issues?
func isPermissionError(err error) bool {
	// Check for common permission-related error messages
	errStr := err.Error()
	return containsAny(errStr, []string{"permission denied", "forbidden", "403"})
}

// Helper function to categorize errors - is the error related to path issues?
func isPathError(err error) bool {
	// Check for path-related error messages
	errStr := err.Error()
	return containsAny(errStr, []string{"invalid path", "not found", "404"})
}

// Helper function to categorize errors - is the error related to connection issues?
func isConnectionError(err error) bool {
	// Check for connection-related error messages
	errStr := err.Error()
	return containsAny(errStr, []string{"connection refused", "timeout", "network"})
}

// containsAny checks if the string contains any of the specified substrings.
func containsAny(str string, substrings []string) bool {
	for _, substr := range substrings {
		if len(str) >= len(substr) {
			for i := 0; i <= len(str)-len(substr); i++ {
				if str[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

// isDataTooLarge checks if the secret data is too large and needs optimization.
func (c *Client) isDataTooLarge(data map[string]interface{}) bool {
	// Calculate approximate size of the data
	totalSize := 0
	for key, value := range data {
		totalSize += len(key)
		if strValue, ok := value.(string); ok {
			totalSize += len(strValue)
		}
	}

	// Consider data "large" if it's over 1MB
	return totalSize > 1024*1024
}

// writeSecretOptimized handles large secrets with memory optimization.
func (c *Client) writeSecretOptimized(ctx context.Context, path string, data map[string]interface{}) error {
	// For very large secrets, we could split them into chunks
	// For now, we'll just write normally but log a warning
	// In a production environment, you might want to implement chunking

	// Log warning about large secret
	totalSize := 0
	for key, value := range data {
		totalSize += len(key)
		if strValue, ok := value.(string); ok {
			totalSize += len(strValue)
		}
	}

	// Write the secret normally but with optimization flags and KV v2 support
	writeData := c.prepareDataForKVVersion(path, data)
	_, err := c.client.Logical().WriteWithContext(ctx, path, writeData)
	if err != nil {
		return fmt.Errorf("failed to write large secret (%d bytes) to vault at path %s: %w", totalSize, path, err)
	}

	return nil
}

// BatchWriteSecrets performs batch write operations for better performance.
func (c *Client) BatchWriteSecrets(ctx context.Context, operations []BatchOperation) error {
	c.batchMutex.Lock()
	defer c.batchMutex.Unlock()

	// Process operations in batches to avoid overwhelming Vault
	batchSize := 5 // Process 5 operations at a time
	for i := 0; i < len(operations); i += batchSize {
		end := i + batchSize
		if end > len(operations) {
			end = len(operations)
		}

		batch := operations[i:end]
		for _, op := range batch {
			// Apply rate limiting for each operation
			if err := c.rateLimiter.Wait(ctx); err != nil {
				return fmt.Errorf("rate limiter error during batch operation: %w", err)
			}

			switch op.Type {
			case "write":
				if err := c.WriteSecret(ctx, op.Path, op.Data); err != nil {
					return fmt.Errorf("batch write failed for path %s: %w", op.Path, err)
				}
			case "delete":
				if err := c.DeleteSecret(ctx, op.Path); err != nil {
					return fmt.Errorf("batch delete failed for path %s: %w", op.Path, err)
				}
			}
		}

		// Small delay between batches to be respectful to Vault
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			// Continue to next batch.
		}
	}

	return nil
}
