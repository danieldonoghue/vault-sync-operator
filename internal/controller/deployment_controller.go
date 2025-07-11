// Package controller contains the Kubernetes controller logic for the vault-sync-operator.
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

// VaultPathAnnotation specifies the Vault path for secret retrieval.
const (
	VaultPathAnnotation             = "vault-sync.io/path"
	VaultSecretsAnnotation          = "vault-sync.io/secrets" //nolint:gosec // This is an annotation name, not a credential
	VaultPreserveOnDeleteAnnotation = "vault-sync.io/preserve-on-delete"
	VaultSecretVersionsAnnotation   = "vault-sync.io/secret-versions" //nolint:gosec // This is an annotation name, not a credential
	VaultRotationCheckAnnotation    = "vault-sync.io/rotation-check"  // Control rotation detection (enabled|disabled|<frequency>)
)

// VaultSyncFinalizer is the finalizer name used by the operator.
const VaultSyncFinalizer = "vault-sync.io/finalizer"

// DefaultRotationCheckFrequency is the default rotation check frequency for future periodic checks.
const DefaultRotationCheckFrequency = "5m"

// DeploymentReconciler reconciles a Deployment object.
type DeploymentReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Log         logr.Logger
	VaultClient *vault.Client
	ClusterName string // Optional cluster identifier for multi-cluster Vault paths
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

// handleDeletion handles the deletion of secrets from Vault when a deployment is deleted.
func (r *DeploymentReconciler) handleDeletion(ctx context.Context, deployment *appsv1.Deployment) (ctrl.Result, error) {
	log := r.Log.WithValues("deployment", deployment.Name, "namespace", deployment.Namespace)

	if controllerutil.ContainsFinalizer(deployment, VaultSyncFinalizer) {
		// Check if deletion should be preserved
		preserveOnDelete := deployment.Annotations[VaultPreserveOnDeleteAnnotation] == "true"

		// Get the vault path
		vaultPath, exists := deployment.Annotations[VaultPathAnnotation]
		if exists && vaultPath != "" && !preserveOnDelete {
			// Add cluster prefix if cluster name is configured
			if r.ClusterName != "" {
				vaultPath = fmt.Sprintf("clusters/%s/%s", r.ClusterName, vaultPath)
			}

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
		} else if preserveOnDelete {
			log.Info("preserving vault secret due to preserve annotation",
				"path", vaultPath,
				"deployment", deployment.Name,
				"namespace", deployment.Namespace,
				"preserve_annotation", "true")
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(deployment, VaultSyncFinalizer)
		return ctrl.Result{}, r.Update(ctx, deployment)
	}

	return ctrl.Result{}, nil
}

// syncSecretsToVault syncs the specified secrets to Vault.
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

	// Add cluster prefix if cluster name is configured
	if r.ClusterName != "" {
		vaultPath = fmt.Sprintf("clusters/%s/%s", r.ClusterName, vaultPath)
	}

	// Check if custom secrets configuration is provided
	secretsToSync, hasCustomConfig := deployment.Annotations[VaultSecretsAnnotation]

	var vaultData map[string]interface{}
	var currentSecretVersions map[string]string
	var err error

	if hasCustomConfig && secretsToSync != "" {
		// Use custom configuration
		log.Info("using custom secret configuration", "config", secretsToSync)
		vaultData, currentSecretVersions, err = r.syncCustomSecretsWithVersions(ctx, deployment, secretsToSync)
		if err != nil {
			metrics.SecretsyncAttempts.WithLabelValues(deployment.Namespace, deployment.Name, "failed").Inc()
			log.Error(err, "failed to sync custom secrets")
			return ctrl.Result{}, err
		}
	} else {
		// Auto-discover secrets from deployment pod template
		log.Info("using auto-discovery mode")
		currentSecretVersions, err = r.syncAutoDiscoveredSecretsToSubPaths(ctx, deployment, vaultPath)
		if err != nil {
			metrics.SecretsyncAttempts.WithLabelValues(deployment.Namespace, deployment.Name, "failed").Inc()
			log.Error(err, "failed to sync auto-discovered secrets")
			return ctrl.Result{}, err
		}
		// In auto-discovery mode, secrets are written to individual sub-paths
		vaultData = make(map[string]interface{})
	}

	// Check if secret versions have changed (rotation detection)
	lastKnownVersions := r.getLastKnownSecretVersions(deployment)
	var hasChanges bool

	// Check if rotation detection is disabled
	if r.isRotationCheckDisabled(deployment) {
		log.Info("secret rotation check disabled, performing sync anyway")
		hasChanges = true
	} else {
		hasChanges = r.detectSecretChanges(lastKnownVersions, currentSecretVersions)
	}

	if !hasChanges && len(lastKnownVersions) > 0 {
		log.Info("no secret changes detected, skipping vault sync",
			"last_versions", lastKnownVersions,
			"current_versions", currentSecretVersions)
		return ctrl.Result{}, nil
	}

	if hasChanges {
		log.Info("secret rotation detected, syncing to vault",
			"changed_secrets", r.getChangedSecrets(lastKnownVersions, currentSecretVersions))
	}

	// Log what we're about to sync
	log.Info("syncing secrets to vault",
		"path", vaultPath,
		"secret_count", len(vaultData),
		"mode", map[bool]string{true: "custom", false: "auto-discovery"}[hasCustomConfig && secretsToSync != ""])

	// Write to Vault (batch operation for performance)
	// Skip writing for auto-discovery mode as secrets are already written to sub-paths
	if len(vaultData) > 0 {
		if err := r.VaultClient.WriteSecret(ctx, vaultPath, vaultData); err != nil {
			metrics.SecretsyncAttempts.WithLabelValues(deployment.Namespace, deployment.Name, "failed").Inc()
			log.Error(err, "failed to write secret to vault",
				"path", vaultPath,
				"secret_count", len(vaultData),
				"error_details", err.Error())
			return ctrl.Result{}, fmt.Errorf("failed to write secret to vault: %w", err)
		}
	}

	// Update secret versions annotation for future rotation detection
	err = r.updateSecretVersionsAnnotation(ctx, deployment, currentSecretVersions)
	if err != nil {
		log.Error(err, "failed to update secret versions annotation", "versions", currentSecretVersions)
		// Don't fail the whole operation for annotation update failure
	}

	// Success metrics and logging
	metrics.SecretsyncAttempts.WithLabelValues(deployment.Namespace, deployment.Name, "success").Inc()
	log.Info("successfully synced secrets to vault",
		"path", vaultPath,
		"secret_count", len(vaultData),
		"duration_seconds", time.Since(start).Seconds())
	return ctrl.Result{}, nil
}

// syncCustomSecretsWithVersions handles custom secret configuration and returns version information.
func (r *DeploymentReconciler) syncCustomSecretsWithVersions(ctx context.Context, deployment *appsv1.Deployment, secretsConfig string) (map[string]interface{}, map[string]string, error) {
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
		return nil, nil, fmt.Errorf("failed to parse secrets annotation: %w", err)
	}

	log.Info("parsed custom secret configuration", "secret_configs", len(secretConfigs))

	// Collect all secret data and versions
	vaultData := make(map[string]interface{})
	secretVersions := make(map[string]string)

	for _, secretConfig := range secretConfigs {
		secret := &corev1.Secret{}
		secretKey := types.NamespacedName{
			Name:      secretConfig.Name,
			Namespace: deployment.Namespace,
		}

		if err := r.Get(ctx, secretKey, secret); err != nil {
			metrics.SecretNotFoundErrors.WithLabelValues(deployment.Namespace, secretConfig.Name).Inc()
			log.Error(err, "failed to get secret - it may be generated by kustomize or similar tools",
				"secret", secretConfig.Name,
				"namespace", deployment.Namespace,
				"deployment", deployment.Name,
				"suggestion", "ensure secret generators run before operator sync")
			return nil, nil, fmt.Errorf("failed to get secret %s (check if secret generators have run): %w", secretConfig.Name, err)
		}

		// Track secret version for rotation detection
		secretVersions[secretConfig.Name] = secret.ResourceVersion

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
				return nil, nil, fmt.Errorf("key %s not found in secret %s", key, secretConfig.Name)
			}
		}
	}

	return vaultData, secretVersions, nil
}


// syncAutoDiscoveredSecretsToSubPaths auto-discovers secrets and writes each to its own sub-path.
func (r *DeploymentReconciler) syncAutoDiscoveredSecretsToSubPaths(ctx context.Context, deployment *appsv1.Deployment, basePath string) (map[string]string, error) {
	log := r.Log.WithValues("deployment", deployment.Name, "namespace", deployment.Namespace)

	// Extract secret names from the deployment pod template
	secretNames := r.extractSecretNamesFromPodTemplate(deployment.Spec.Template)

	if len(secretNames) == 0 {
		log.Info("no secrets found in deployment pod template")
		return map[string]string{}, nil
	}

	log.Info("auto-discovered secrets", "secrets", secretNames)

	// Track discovered secrets metric
	metrics.SecretsDiscovered.WithLabelValues(deployment.Namespace, deployment.Name).Set(float64(len(secretNames)))

	// Collect secret versions and write each secret to its own sub-path
	secretVersions := make(map[string]string)

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

		// Track secret version for rotation detection
		secretVersions[secretName] = secret.ResourceVersion

		// Create vault data for this secret (flattened structure)
		secretData := make(map[string]interface{})
		for key, value := range secret.Data {
			secretData[key] = string(value)
		}

		// Write to sub-path: basePath/secretName
		secretPath := fmt.Sprintf("%s/%s", basePath, secretName)
		
		log.Info("writing secret to vault sub-path", 
			"secret", secretName,
			"path", secretPath,
			"keys", len(secretData))

		if err := r.VaultClient.WriteSecret(ctx, secretPath, secretData); err != nil {
			log.Error(err, "failed to write secret to vault sub-path",
				"secret", secretName,
				"path", secretPath,
				"error_details", err.Error())
			return nil, fmt.Errorf("failed to write secret %s to vault: %w", secretName, err)
		}
	}

	return secretVersions, nil
}

// extractSecretNamesFromPodTemplate extracts all secret names referenced in the pod template.
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

// getSecretKeys returns a slice of keys available in a secret's data.
func getSecretKeys(data map[string][]byte) []string {
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	return keys
}

// SecretConfig defines which keys from a secret to sync to Vault.
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

// getLastKnownSecretVersions retrieves the last known secret versions from deployment annotations.
func (r *DeploymentReconciler) getLastKnownSecretVersions(deployment *appsv1.Deployment) map[string]string {
	versionsAnnotation, exists := deployment.Annotations[VaultSecretVersionsAnnotation]
	if !exists || versionsAnnotation == "" {
		return make(map[string]string)
	}

	var versions map[string]string
	if err := json.Unmarshal([]byte(versionsAnnotation), &versions); err != nil {
		r.Log.Error(err, "failed to parse secret versions annotation",
			"annotation", versionsAnnotation,
			"deployment", deployment.Name,
			"namespace", deployment.Namespace)
		return make(map[string]string)
	}

	return versions
}

// detectSecretChanges compares last known versions with current versions to detect changes.
func (r *DeploymentReconciler) detectSecretChanges(lastVersions, currentVersions map[string]string) bool {
	// If no previous versions exist, consider it a change (initial sync)
	if len(lastVersions) == 0 {
		return true
	}

	// Check if any secret version has changed
	for secretName, currentVersion := range currentVersions {
		if lastVersion, exists := lastVersions[secretName]; !exists || lastVersion != currentVersion {
			return true
		}
	}

	// Check if any secret was removed
	for secretName := range lastVersions {
		if _, exists := currentVersions[secretName]; !exists {
			return true
		}
	}

	return false
}

// getChangedSecrets returns a list of secrets that have changed versions.
func (r *DeploymentReconciler) getChangedSecrets(lastVersions, currentVersions map[string]string) []string {
	var changed []string

	// Find changed secrets
	for secretName, currentVersion := range currentVersions {
		if lastVersion, exists := lastVersions[secretName]; !exists || lastVersion != currentVersion {
			changed = append(changed, secretName)
		}
	}

	// Find removed secrets
	for secretName := range lastVersions {
		if _, exists := currentVersions[secretName]; !exists {
			changed = append(changed, secretName+" (removed)")
		}
	}

	return changed
}

// updateSecretVersionsAnnotation updates the deployment with current secret versions.
func (r *DeploymentReconciler) updateSecretVersionsAnnotation(ctx context.Context, deployment *appsv1.Deployment, versions map[string]string) error {
	versionsJSON, err := json.Marshal(versions)
	if err != nil {
		return fmt.Errorf("failed to marshal secret versions: %w", err)
	}

	// Create a copy of the deployment to update
	updatedDeployment := deployment.DeepCopy()
	if updatedDeployment.Annotations == nil {
		updatedDeployment.Annotations = make(map[string]string)
	}
	updatedDeployment.Annotations[VaultSecretVersionsAnnotation] = string(versionsJSON)

	// Update the deployment
	if err := r.Update(ctx, updatedDeployment); err != nil {
		return fmt.Errorf("failed to update deployment annotations: %w", err)
	}

	return nil
}

// isRotationCheckDisabled checks if secret rotation detection is disabled for this deployment.
func (r *DeploymentReconciler) isRotationCheckDisabled(deployment *appsv1.Deployment) bool {
	rotationCheck, exists := deployment.Annotations[VaultRotationCheckAnnotation]
	return exists && rotationCheck == "disabled"
}
