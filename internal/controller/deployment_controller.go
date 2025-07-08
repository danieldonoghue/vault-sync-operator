package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/danieldonoghue/vault-sync-operator/internal/metrics"
	"github.com/danieldonoghue/vault-sync-operator/internal/vault"
)

const (
	// Annotations used by the operator
	VaultPathAnnotation    = "vault-sync.io/path"
	VaultSecretsAnnotation = "vault-sync.io/secrets"

	// Finalizer name
	VaultSyncFinalizer = "vault-sync.io/finalizer"
)

// DeploymentReconciler reconciles a Deployment object
type DeploymentReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Log         logr.Logger
	VaultClient *vault.Client
}

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("deployment", req.NamespacedName)

	// Fetch the Deployment instance
	deployment := &appsv1.Deployment{}
	err := r.Get(ctx, req.NamespacedName, deployment)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Deployment not found, probably deleted
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Deployment")
		return ctrl.Result{}, err
	}

	// Check if vault-sync is enabled for this deployment (presence of vault path annotation)
	vaultPath, vaultSyncEnabled := deployment.Annotations[VaultPathAnnotation]
	if !vaultSyncEnabled || vaultPath == "" {
		// Remove finalizer if it exists but sync is disabled
		if controllerutil.ContainsFinalizer(deployment, VaultSyncFinalizer) {
			controllerutil.RemoveFinalizer(deployment, VaultSyncFinalizer)
			return ctrl.Result{}, r.Update(ctx, deployment)
		}
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if deployment.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, deployment)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(deployment, VaultSyncFinalizer) {
		controllerutil.AddFinalizer(deployment, VaultSyncFinalizer)
		return ctrl.Result{}, r.Update(ctx, deployment)
	}

	// Sync secrets to Vault
	return r.syncSecretsToVault(ctx, deployment)
}

// handleDeletion handles the deletion of secrets from Vault when a deployment is deleted
func (r *DeploymentReconciler) handleDeletion(ctx context.Context, deployment *appsv1.Deployment) (ctrl.Result, error) {
	log := r.Log.WithValues("deployment", deployment.Name, "namespace", deployment.Namespace)

	if controllerutil.ContainsFinalizer(deployment, VaultSyncFinalizer) {
		// Get the vault path
		vaultPath, exists := deployment.Annotations[VaultPathAnnotation]
		if exists && vaultPath != "" {
			// Delete the secret from Vault
			if err := r.VaultClient.DeleteSecret(ctx, vaultPath); err != nil {
				log.Error(err, "failed to delete secret from vault",
					"path", vaultPath,
					"deployment", deployment.Name,
					"namespace", deployment.Namespace,
					"error_details", err.Error())
				return ctrl.Result{}, err
			}
			log.Info("successfully deleted secret from vault",
				"path", vaultPath,
				"deployment", deployment.Name,
				"namespace", deployment.Namespace)
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(deployment, VaultSyncFinalizer)
		return ctrl.Result{}, r.Update(ctx, deployment)
	}

	return ctrl.Result{}, nil
}

// syncSecretsToVault syncs the specified secrets to Vault
func (r *DeploymentReconciler) syncSecretsToVault(ctx context.Context, deployment *appsv1.Deployment) (ctrl.Result, error) {
	log := r.Log.WithValues("deployment", deployment.Name, "namespace", deployment.Namespace)

	// Start timing the operation
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		metrics.SecretsyncDuration.WithLabelValues(deployment.Namespace, deployment.Name).Observe(duration)
	}()

	// Get the vault path (we already know it exists from reconcile check)
	vaultPath := deployment.Annotations[VaultPathAnnotation]

	// Check if custom secrets configuration is provided
	secretsToSync, hasCustomConfig := deployment.Annotations[VaultSecretsAnnotation]

	var vaultData map[string]interface{}
	var err error

	if hasCustomConfig && secretsToSync != "" {
		// Use custom configuration
		log.Info("using custom secret configuration", "config", secretsToSync)
		vaultData, err = r.syncCustomSecrets(ctx, deployment, secretsToSync)
		if err != nil {
			metrics.SecretsyncAttempts.WithLabelValues(deployment.Namespace, deployment.Name, "failed").Inc()
			log.Error(err, "failed to sync custom secrets")
			return ctrl.Result{}, err
		}
	} else {
		// Auto-discover secrets from deployment pod template
		log.Info("using auto-discovery mode")
		vaultData, err = r.syncAutoDiscoveredSecrets(ctx, deployment)
		if err != nil {
			metrics.SecretsyncAttempts.WithLabelValues(deployment.Namespace, deployment.Name, "failed").Inc()
			log.Error(err, "failed to sync auto-discovered secrets")
			return ctrl.Result{}, err
		}
	}

	// Log what we're about to sync
	log.Info("syncing secrets to vault",
		"path", vaultPath,
		"secret_count", len(vaultData),
		"mode", map[bool]string{true: "custom", false: "auto-discovery"}[hasCustomConfig && secretsToSync != ""])

	// Write to Vault
	if err := r.VaultClient.WriteSecret(ctx, vaultPath, vaultData); err != nil {
		metrics.SecretsyncAttempts.WithLabelValues(deployment.Namespace, deployment.Name, "failed").Inc()
		log.Error(err, "failed to write secret to vault",
			"path", vaultPath,
			"secret_count", len(vaultData),
			"error_details", err.Error())
		return ctrl.Result{}, fmt.Errorf("failed to write secret to vault: %w", err)
	}

	// Success metrics and logging
	metrics.SecretsyncAttempts.WithLabelValues(deployment.Namespace, deployment.Name, "success").Inc()
	log.Info("successfully synced secrets to vault",
		"path", vaultPath,
		"secret_count", len(vaultData),
		"duration_seconds", time.Since(start).Seconds())
	return ctrl.Result{}, nil
}

// syncCustomSecrets handles custom secret configuration
func (r *DeploymentReconciler) syncCustomSecrets(ctx context.Context, deployment *appsv1.Deployment, secretsConfig string) (map[string]interface{}, error) {
	log := r.Log.WithValues("deployment", deployment.Name, "namespace", deployment.Namespace)

	// Parse the secrets annotation (JSON format)
	var secretConfigs []SecretConfig
	if err := json.Unmarshal([]byte(secretsConfig), &secretConfigs); err != nil {
		metrics.ConfigParseErrors.WithLabelValues(deployment.Namespace, deployment.Name, "json_parse_error").Inc()
		log.Error(err, "failed to parse secrets annotation",
			"annotation", secretsConfig,
			"error_type", "json_parse_error",
			"deployment", deployment.Name,
			"namespace", deployment.Namespace)
		return nil, fmt.Errorf("failed to parse secrets annotation: %w", err)
	}

	log.Info("parsed custom secret configuration", "secret_configs", len(secretConfigs))

	// Collect all secret data
	vaultData := make(map[string]interface{})

	for _, secretConfig := range secretConfigs {
		secret := &corev1.Secret{}
		secretKey := types.NamespacedName{
			Name:      secretConfig.Name,
			Namespace: deployment.Namespace,
		}

		if err := r.Get(ctx, secretKey, secret); err != nil {
			metrics.SecretNotFoundErrors.WithLabelValues(deployment.Namespace, secretConfig.Name).Inc()
			log.Error(err, "failed to get secret",
				"secret", secretConfig.Name,
				"namespace", deployment.Namespace,
				"deployment", deployment.Name)
			return nil, fmt.Errorf("failed to get secret %s: %w", secretConfig.Name, err)
		}

		// Add specified keys to vault data
		for _, key := range secretConfig.Keys {
			if data, exists := secret.Data[key]; exists {
				// Use prefix if specified
				vaultKey := key
				if secretConfig.Prefix != "" {
					vaultKey = secretConfig.Prefix + key
				}
				vaultData[vaultKey] = string(data)
			} else {
				metrics.SecretKeyMissingError.WithLabelValues(deployment.Namespace, secretConfig.Name, key).Inc()
				log.Error(fmt.Errorf("key not found in secret"), "key not found",
					"secret", secretConfig.Name,
					"key", key,
					"available_keys", getSecretKeys(secret.Data),
					"namespace", deployment.Namespace,
					"deployment", deployment.Name)
				return nil, fmt.Errorf("key %s not found in secret %s", key, secretConfig.Name)
			}
		}
	}

	return vaultData, nil
}

// syncAutoDiscoveredSecrets auto-discovers and syncs all secrets referenced in the deployment
func (r *DeploymentReconciler) syncAutoDiscoveredSecrets(ctx context.Context, deployment *appsv1.Deployment) (map[string]interface{}, error) {
	log := r.Log.WithValues("deployment", deployment.Name, "namespace", deployment.Namespace)

	// Extract secret names from the deployment pod template
	secretNames := r.extractSecretNamesFromPodTemplate(deployment.Spec.Template)

	if len(secretNames) == 0 {
		log.Info("no secrets found in deployment pod template")
		return map[string]interface{}{}, nil
	}

	log.Info("auto-discovered secrets", "secrets", secretNames)

	// Track discovered secrets metric
	metrics.SecretsDiscovered.WithLabelValues(deployment.Namespace, deployment.Name).Set(float64(len(secretNames)))

	// Collect all secret data
	vaultData := make(map[string]interface{})

	for secretName := range secretNames {
		secret := &corev1.Secret{}
		secretKey := types.NamespacedName{
			Name:      secretName,
			Namespace: deployment.Namespace,
		}

		if err := r.Get(ctx, secretKey, secret); err != nil {
			metrics.SecretNotFoundErrors.WithLabelValues(deployment.Namespace, secretName).Inc()
			log.Error(err, "failed to get auto-discovered secret",
				"secret", secretName,
				"namespace", deployment.Namespace,
				"deployment", deployment.Name)
			return nil, fmt.Errorf("failed to get secret %s: %w", secretName, err)
		}

		// Create a nested object for this secret
		secretData := make(map[string]interface{})
		for key, value := range secret.Data {
			secretData[key] = string(value)
		}

		// Store the entire secret as a nested object
		vaultData[secretName] = secretData
	}

	return vaultData, nil
}

// extractSecretNamesFromPodTemplate extracts all secret names referenced in the pod template
func (r *DeploymentReconciler) extractSecretNamesFromPodTemplate(podTemplate corev1.PodTemplateSpec) map[string]bool {
	secretNames := make(map[string]bool)

	// Check environment variables
	for _, container := range podTemplate.Spec.Containers {
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				secretNames[env.ValueFrom.SecretKeyRef.Name] = true
			}
		}

		// Check envFrom
		for _, envFrom := range container.EnvFrom {
			if envFrom.SecretRef != nil {
				secretNames[envFrom.SecretRef.Name] = true
			}
		}
	}

	// Check init containers
	for _, container := range podTemplate.Spec.InitContainers {
		for _, env := range container.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				secretNames[env.ValueFrom.SecretKeyRef.Name] = true
			}
		}

		for _, envFrom := range container.EnvFrom {
			if envFrom.SecretRef != nil {
				secretNames[envFrom.SecretRef.Name] = true
			}
		}
	}

	// Check volumes
	for _, volume := range podTemplate.Spec.Volumes {
		if volume.Secret != nil {
			secretNames[volume.Secret.SecretName] = true
		}
	}

	return secretNames
}

// getSecretKeys returns a slice of keys available in a secret's data
func getSecretKeys(data map[string][]byte) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	return keys
}

// SecretConfig defines which keys from a secret to sync to Vault
type SecretConfig struct {
	Name   string   `json:"name"`
	Keys   []string `json:"keys"`
	Prefix string   `json:"prefix,omitempty"`
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Complete(r)
}
