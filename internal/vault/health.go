package vault

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// HealthCheck performs a health check against the Vault connection.
func (c *Client) HealthCheck(ctx context.Context) error {
	// Create a timeout context for the health check
	healthCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Check if we can reach Vault's health endpoint
	resp, err := c.client.Logical().ReadRawWithContext(healthCtx, "sys/health")
	if err != nil {
		return fmt.Errorf("vault health check failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check if Vault is sealed or in standby (both are ok for health check)
	if resp.StatusCode != http.StatusOK &&
		resp.StatusCode != http.StatusTooManyRequests && // standby
		resp.StatusCode != http.StatusServiceUnavailable { // sealed
		return fmt.Errorf("vault health check returned unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// ReadinessCheck performs a more thorough readiness check including authentication.
func (c *Client) ReadinessCheck(ctx context.Context) error {
	// First do the basic health check
	if err := c.HealthCheck(ctx); err != nil {
		return err
	}

	// Check if we have a valid token
	if c.client.Token() == "" {
		return fmt.Errorf("vault client not authenticated")
	}

	// Try to read our own token info to verify authentication works
	readinessCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := c.client.Auth().Token().LookupSelfWithContext(readinessCtx)
	if err != nil {
		return fmt.Errorf("vault authentication check failed: %w", err)
	}

	return nil
}
