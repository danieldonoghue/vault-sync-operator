package vault

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/danieldonoghue/vault-sync-operator/internal/metrics"
	"github.com/hashicorp/vault/api"
)

// Client represents a Vault client with Kubernetes authentication
type Client struct {
	client   *api.Client
	role     string
	authPath string
}

// NewClient creates a new Vault client with Kubernetes authentication
func NewClient(vaultAddr, role, authPath string) (*Client, error) {
	config := api.DefaultConfig()
	config.Address = vaultAddr

	client, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create vault client: %w", err)
	}

	vaultClient := &Client{
		client:   client,
		role:     role,
		authPath: authPath,
	}

	// Authenticate with Kubernetes auth method
	if err := vaultClient.authenticate(); err != nil {
		return nil, fmt.Errorf("failed to authenticate with vault: %w", err)
	}

	return vaultClient, nil
}

// authenticate performs Kubernetes authentication with Vault
func (c *Client) authenticate() error {
	// Read the service account token
	tokenPath := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	jwt, err := ioutil.ReadFile(tokenPath)
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

// WriteSecret writes a secret to Vault at the specified path
func (c *Client) WriteSecret(ctx context.Context, path string, data map[string]interface{}) error {
	// Ensure we have a valid token
	if c.client.Token() == "" {
		if err := c.authenticate(); err != nil {
			metrics.VaultWriteErrors.WithLabelValues("auth_failed", path).Inc()
			return fmt.Errorf("failed to re-authenticate: %w", err)
		}
	}

	// Write the secret
	_, err := c.client.Logical().WriteWithContext(ctx, path, data)
	if err != nil {
		// Categorize the error type for better metrics
		errorType := "unknown"
		if isPermissionError(err) {
			errorType = "permission_denied"
		} else if isPathError(err) {
			errorType = "invalid_path"
		} else if isConnectionError(err) {
			errorType = "connection_failed"
		}

		metrics.VaultWriteErrors.WithLabelValues(errorType, path).Inc()
		return fmt.Errorf("failed to write secret to vault at path %s: %w", path, err)
	}

	return nil
}

// DeleteSecret deletes a secret from Vault at the specified path
func (c *Client) DeleteSecret(ctx context.Context, path string) error {
	// Ensure we have a valid token
	if c.client.Token() == "" {
		if err := c.authenticate(); err != nil {
			return fmt.Errorf("failed to re-authenticate: %w", err)
		}
	}

	// Delete the secret
	_, err := c.client.Logical().DeleteWithContext(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to delete secret from vault at path %s: %w", path, err)
	}

	return nil
}

// Helper functions to categorize errors
func isPermissionError(err error) bool {
	// Check for common permission-related error messages
	errStr := err.Error()
	return containsAny(errStr, []string{"permission denied", "forbidden", "403"})
}

func isPathError(err error) bool {
	// Check for path-related error messages
	errStr := err.Error()
	return containsAny(errStr, []string{"invalid path", "not found", "404"})
}

func isConnectionError(err error) bool {
	// Check for connection-related error messages
	errStr := err.Error()
	return containsAny(errStr, []string{"connection refused", "timeout", "network"})
}

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
