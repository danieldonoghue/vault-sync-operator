package controller

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/danield/vault-sync-operator/internal/vault"
)

const (
	// Annotations used by the operator
	VaultSyncEnabledAnnotation = "vault-sync.io/enabled"
	VaultPathAnnotation        = "vault-sync.io/path"
	VaultSecretsAnnotation     = "vault-sync.io/secrets"

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

	// Check if vault-sync is enabled for this deployment
	enabled, exists := deployment.Annotations[VaultSyncEnabledAnnotation]
	if !exists || enabled != "true" {
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
				log.Error(err, "failed to delete secret from vault", "path", vaultPath)
				return ctrl.Result{}, err
			}
			log.Info("successfully deleted secret from vault", "path", vaultPath)
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

	// Get the vault path
	vaultPath, exists := deployment.Annotations[VaultPathAnnotation]
	if !exists || vaultPath == "" {
		log.Error(fmt.Errorf("vault path annotation is required"), "missing vault path annotation")
		return ctrl.Result{}, fmt.Errorf("vault path annotation %s is required", VaultPathAnnotation)
	}

	// Get the secrets to sync
	secretsToSync, exists := deployment.Annotations[VaultSecretsAnnotation]
	if !exists || secretsToSync == "" {
		log.Error(fmt.Errorf("vault secrets annotation is required"), "missing vault secrets annotation")
		return ctrl.Result{}, fmt.Errorf("vault secrets annotation %s is required", VaultSecretsAnnotation)
	}

	// Parse the secrets annotation (JSON format)
	var secretConfigs []SecretConfig
	if err := json.Unmarshal([]byte(secretsToSync), &secretConfigs); err != nil {
		log.Error(err, "failed to parse secrets annotation", "annotation", secretsToSync)
		return ctrl.Result{}, fmt.Errorf("failed to parse secrets annotation: %w", err)
	}

	// Collect all secret data
	vaultData := make(map[string]interface{})

	for _, secretConfig := range secretConfigs {
		secret := &corev1.Secret{}
		secretKey := types.NamespacedName{
			Name:      secretConfig.Name,
			Namespace: deployment.Namespace,
		}

		if err := r.Get(ctx, secretKey, secret); err != nil {
			log.Error(err, "failed to get secret", "secret", secretConfig.Name)
			return ctrl.Result{}, fmt.Errorf("failed to get secret %s: %w", secretConfig.Name, err)
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
				log.Error(fmt.Errorf("key not found in secret"), "key not found", "secret", secretConfig.Name, "key", key)
				return ctrl.Result{}, fmt.Errorf("key %s not found in secret %s", key, secretConfig.Name)
			}
		}
	}

	// Write to Vault
	if err := r.VaultClient.WriteSecret(ctx, vaultPath, vaultData); err != nil {
		log.Error(err, "failed to write secret to vault", "path", vaultPath)
		return ctrl.Result{}, fmt.Errorf("failed to write secret to vault: %w", err)
	}

	log.Info("successfully synced secrets to vault", "path", vaultPath, "secrets", len(vaultData))
	return ctrl.Result{}, nil
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
