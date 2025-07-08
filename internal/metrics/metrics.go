package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// SecretsyncAttempts tracks the number of secret sync attempts
	SecretsyncAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_sync_operator_sync_attempts_total",
			Help: "Total number of secret sync attempts",
		},
		[]string{"namespace", "deployment", "result"},
	)

	// SecretsyncDuration tracks the duration of secret sync operations
	SecretsyncDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vault_sync_operator_sync_duration_seconds",
			Help:    "Duration of secret sync operations in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "deployment"},
	)

	// VaultAuthAttempts tracks Vault authentication attempts
	VaultAuthAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_sync_operator_auth_attempts_total",
			Help: "Total number of Vault authentication attempts",
		},
		[]string{"result"},
	)

	// SecretsDiscovered tracks the number of auto-discovered secrets
	SecretsDiscovered = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vault_sync_operator_secrets_discovered",
			Help: "Number of secrets discovered in deployments",
		},
		[]string{"namespace", "deployment"},
	)

	// VaultWriteErrors tracks Vault write errors by type
	VaultWriteErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_sync_operator_vault_write_errors_total",
			Help: "Total number of Vault write errors by type",
		},
		[]string{"error_type", "path"},
	)

	// SecretNotFoundErrors tracks secret not found errors
	SecretNotFoundErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_sync_operator_secret_not_found_errors_total",
			Help: "Total number of secret not found errors",
		},
		[]string{"namespace", "secret_name"},
	)

	// SecretKeyMissingError tracks missing keys in secrets
	SecretKeyMissingError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_sync_operator_secret_key_missing_errors_total",
			Help: "Total number of missing key errors in secrets",
		},
		[]string{"namespace", "secret_name", "key"},
	)

	// ConfigParseErrors tracks configuration parsing errors
	ConfigParseErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_sync_operator_config_parse_errors_total",
			Help: "Total number of configuration parsing errors",
		},
		[]string{"namespace", "deployment", "error_type"},
	)

	// RuntimeInfo provides information about Go runtime configuration
	RuntimeInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vault_sync_operator_runtime_info",
			Help: "Go runtime configuration information",
		},
		[]string{"setting", "value"},
	)
)

func init() {
	// Register metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		SecretsyncAttempts,
		SecretsyncDuration,
		VaultAuthAttempts,
		SecretsDiscovered,
		VaultWriteErrors,
		SecretNotFoundErrors,
		SecretKeyMissingError,
		ConfigParseErrors,
		RuntimeInfo,
	)
}
