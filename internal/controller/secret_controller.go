// Package controller contains the Kubernetes controller logic for the vault-sync-operator.
// This file implements the SecretReconciler which handles Secret resources with vault-sync annotations.
package controller

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/danieldonoghue/vault-sync-operator/internal/vault"
)

// SecretReconciler reconciles a Secret object.
type SecretReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Log         logr.Logger
	VaultClient *vault.Client
	ClusterName string // Optional cluster identifier for multi-cluster Vault paths
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=secrets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("secret", req.NamespacedName)

	// Fetch the Secret instance
	secret := &corev1.Secret{}
	err := r.Get(ctx, req.NamespacedName, secret)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Secret not found, probably deleted
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Secret")
		return ctrl.Result{}, err
	}

	// Check if vault-sync is enabled for this secret (presence of vault path annotation)
	vaultPath, vaultSyncEnabled := secret.Annotations[VaultPathAnnotation]
	if !vaultSyncEnabled || vaultPath == "" {
		// Remove finalizer if it exists but sync is disabled
		if controllerutil.ContainsFinalizer(secret, VaultSyncFinalizer) {
			controllerutil.RemoveFinalizer(secret, VaultSyncFinalizer)
			return ctrl.Result{}, r.Update(ctx, secret)
		}
		return ctrl.Result{}, nil
	}

	// Handle deletion
	if secret.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, secret)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(secret, VaultSyncFinalizer) {
		controllerutil.AddFinalizer(secret, VaultSyncFinalizer)
		return ctrl.Result{}, r.Update(ctx, secret)
	}

	// Sync secret to Vault
	result, err := r.syncSecretToVault(ctx, secret)
	if err != nil {
		return result, err
	}

	// Check if periodic reconciliation is enabled
	reconcileInterval := r.getReconcileInterval(secret)
	if reconcileInterval > 0 {
		log.V(1).Info("periodic reconciliation enabled", 
			"interval", reconcileInterval,
			"next_reconcile", time.Now().Add(reconcileInterval))
		result.RequeueAfter = reconcileInterval
	}

	return result, nil
}

// handleDeletion handles the deletion of secrets from Vault when a secret is deleted.
func (r *SecretReconciler) handleDeletion(ctx context.Context, secret *corev1.Secret) (ctrl.Result, error) {
	log := r.Log.WithValues("secret", secret.Name, "namespace", secret.Namespace)

	if controllerutil.ContainsFinalizer(secret, VaultSyncFinalizer) {
		// Check if deletion should be preserved
		preserveOnDelete := secret.Annotations[VaultPreserveOnDeleteAnnotation] == "true"

		// Get the vault path
		vaultPath, exists := secret.Annotations[VaultPathAnnotation]
		if exists && vaultPath != "" && !preserveOnDelete {
			// Create sync context
			syncCtx := &SyncContext{
				Client:      r.Client,
				VaultClient: r.VaultClient,
				Log:         r.Log,
				ClusterName: r.ClusterName,
			}

			resourceInfo := ResourceInfo{
				Name:      secret.Name,
				Namespace: secret.Namespace,
				Type:      "secret",
			}

			// Delete the secret from Vault
			if err := syncCtx.DeleteSecretFromVault(ctx, vaultPath, resourceInfo); err != nil {
				log.Error(err, "failed to delete secret from vault",
					"path", vaultPath,
					"error_details", err.Error())
				return ctrl.Result{}, err
			}
		} else if preserveOnDelete {
			log.Info("preserving vault secret due to preserve annotation",
				"path", vaultPath,
				"preserve_annotation", "true")
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(secret, VaultSyncFinalizer)
		return ctrl.Result{}, r.Update(ctx, secret)
	}

	return ctrl.Result{}, nil
}

// syncSecretToVault syncs the secret to Vault.
func (r *SecretReconciler) syncSecretToVault(ctx context.Context, secret *corev1.Secret) (ctrl.Result, error) {
	log := r.Log.WithValues("secret", secret.Name, "namespace", secret.Namespace)

	// Get the vault path (we already know it exists from reconcile check)
	vaultPath := secret.Annotations[VaultPathAnnotation]

	// Create sync context
	syncCtx := &SyncContext{
		Client:      r.Client,
		VaultClient: r.VaultClient,
		Log:         r.Log,
		ClusterName: r.ClusterName,
	}

	resourceInfo := ResourceInfo{
		Name:      secret.Name,
		Namespace: secret.Namespace,
		Type:      "secret",
	}

	// Check if custom secrets configuration is provided
	secretsToSync, hasCustomConfig := secret.Annotations[VaultSecretsAnnotation]

	var vaultData map[string]interface{}
	var currentSecretVersions map[string]string
	var err error

	if hasCustomConfig && secretsToSync != "" {
		// Use custom configuration (self-referential for secrets)
		log.Info("using custom secret configuration", "config", secretsToSync)
		vaultData, currentSecretVersions, err = syncCtx.SyncCustomSecretsWithVersions(ctx, resourceInfo, secretsToSync, secret.Namespace)
		if err != nil {
			log.Error(err, "failed to sync custom secret configuration")
			return ctrl.Result{}, err
		}
	} else {
		// Sync all keys from this secret
		log.Info("syncing all secret keys")
		vaultData, currentSecretVersions, err = syncCtx.SyncAllSecretKeys(ctx, resourceInfo, secret)
		if err != nil {
			log.Error(err, "failed to sync all secret keys")
			return ctrl.Result{}, err
		}
	}

	// Check if secret versions have changed (rotation detection)
	lastKnownVersions := r.getLastKnownSecretVersions(secret)
	var hasChanges bool

	// Check if rotation detection is disabled
	if r.isRotationCheckDisabled(secret) {
		log.Info("secret rotation check disabled, performing sync anyway")
		hasChanges = true
	} else {
		hasChanges = syncCtx.DetectSecretChanges(lastKnownVersions, currentSecretVersions)
	}

	if !hasChanges && len(lastKnownVersions) > 0 {
		log.Info("no secret changes detected, skipping vault sync",
			"last_versions", lastKnownVersions,
			"current_versions", currentSecretVersions)
		return ctrl.Result{}, nil
	}

	if hasChanges {
		log.Info("secret rotation detected, syncing to vault",
			"changed_secrets", syncCtx.GetChangedSecrets(lastKnownVersions, currentSecretVersions))
	}

	// Write to Vault
	if err := syncCtx.WriteSecretToVault(ctx, vaultPath, vaultData, resourceInfo); err != nil {
		return ctrl.Result{}, err
	}

	// Update secret versions annotation for future rotation detection
	err = UpdateSecretVersionsAnnotation(ctx, r.Client, secret, currentSecretVersions)
	if err != nil {
		log.Error(err, "failed to update secret versions annotation", "versions", currentSecretVersions)
		// Don't fail the whole operation for annotation update failure
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		Complete(r)
}

// getLastKnownSecretVersions retrieves the last known secret versions from secret annotations.
func (r *SecretReconciler) getLastKnownSecretVersions(secret *corev1.Secret) map[string]string {
	versionsAnnotation, exists := secret.Annotations[VaultSecretVersionsAnnotation]
	if !exists {
		return make(map[string]string)
	}

	return ParseSecretVersionsAnnotation(versionsAnnotation, r.Log, secret.Name, secret.Namespace)
}

// isRotationCheckDisabled checks if secret rotation detection is disabled for this secret.
func (r *SecretReconciler) isRotationCheckDisabled(secret *corev1.Secret) bool {
	rotationCheck, exists := secret.Annotations[VaultRotationCheckAnnotation]
	return exists && rotationCheck == "disabled"
}

// getReconcileInterval parses the reconciliation interval from the vault-sync.io/reconcile annotation.
// Returns the duration if valid, or zero duration if disabled or invalid.
func (r *SecretReconciler) getReconcileInterval(secret *corev1.Secret) time.Duration {
	reconcileValue, exists := secret.Annotations[VaultReconcileAnnotation]
	if !exists || reconcileValue == "" || reconcileValue == "off" {
		return 0 // Disabled
	}
	
	duration, err := time.ParseDuration(reconcileValue)
	if err != nil {
		r.Log.Error(err, "invalid reconcile interval annotation, disabling periodic reconciliation",
			"secret", secret.Name,
			"namespace", secret.Namespace,
			"annotation_value", reconcileValue)
		return 0 // Disabled on parse error
	}
	
	// Enforce minimum interval of 30 seconds to prevent excessive reconciliation
	if duration < 30*time.Second {
		r.Log.Info("reconcile interval too short, using minimum of 30 seconds",
			"secret", secret.Name,
			"namespace", secret.Namespace,
			"requested", duration,
			"enforced", 30*time.Second)
		return 30 * time.Second
	}
	
	return duration
}