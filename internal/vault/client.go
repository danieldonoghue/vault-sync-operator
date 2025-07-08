package vault

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"path/filepath"

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
		return fmt.Errorf("failed to authenticate: %w", err)
	}

	if secret == nil || secret.Auth == nil {
		return errors.New("authentication response was empty")
	}

	// Set the token for future requests
	c.client.SetToken(secret.Auth.ClientToken)

	return nil
}

// WriteSecret writes a secret to Vault at the specified path
func (c *Client) WriteSecret(ctx context.Context, path string, data map[string]interface{}) error {
	// Ensure we have a valid token
	if c.client.Token() == "" {
		if err := c.authenticate(); err != nil {
			return fmt.Errorf("failed to re-authenticate: %w", err)
		}
	}

	// Write the secret
	_, err := c.client.Logical().WriteWithContext(ctx, path, data)
	if err != nil {
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
