package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/danieldonoghue/vault-sync-operator/internal/controller"
	"github.com/danieldonoghue/vault-sync-operator/internal/vault"
)

type IntegrationTestSuite struct {
	vaultContainer testcontainers.Container
	vaultClient    *api.Client
	testEnv        *envtest.Environment
	k8sClient      client.Client
	reconciler     *controller.DeploymentReconciler
	vaultOperator  *vault.Client
}

func TestIntegrationSuite(t *testing.T) {
	suite := &IntegrationTestSuite{}

	// Setup
	err := suite.SetupSuite()
	require.NoError(t, err)
	defer suite.TearDownSuite()

	// Run tests
	t.Run("TestBasicSecretSync", suite.TestBasicSecretSync)
	t.Run("TestCustomSecretConfiguration", suite.TestCustomSecretConfiguration)
	t.Run("TestAutoDiscovery", suite.TestAutoDiscovery)
	t.Run("TestPreserveOnDelete", suite.TestPreserveOnDelete)
	t.Run("TestMultiClusterPaths", suite.TestMultiClusterPaths)
	t.Run("TestVaultAuthFailure", suite.TestVaultAuthFailure)
	t.Run("TestSecretNotFound", suite.TestSecretNotFound)
	t.Run("TestSecretRotationDetection", suite.TestSecretRotationDetection)
	t.Run("TestRotationCheckAnnotation", suite.TestRotationCheckAnnotation)
	t.Run("TestBatchOperations", suite.TestBatchOperations)
	t.Run("TestChaosRecovery", suite.TestChaosRecovery)
}

func (suite *IntegrationTestSuite) SetupSuite() error {
	ctx := context.Background()

	// Start Vault container
	req := testcontainers.ContainerRequest{
		Image:        "vault:1.15",
		ExposedPorts: []string{"8200/tcp"},
		WaitingFor:   wait.ForLog("Vault server started!"),
		Env: map[string]string{
			"VAULT_DEV_ROOT_TOKEN_ID":  "test-token",
			"VAULT_DEV_LISTEN_ADDRESS": "0.0.0.0:8200",
		},
		Cmd: []string{"vault", "server", "-dev"},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return fmt.Errorf("failed to start vault container: %w", err)
	}
	suite.vaultContainer = container

	// Get Vault address
	vaultHost, err := container.Host(ctx)
	if err != nil {
		return err
	}
	vaultPort, err := container.MappedPort(ctx, "8200")
	if err != nil {
		return err
	}
	vaultAddr := fmt.Sprintf("http://%s:%s", vaultHost, vaultPort.Port())

	// Setup Vault client
	config := api.DefaultConfig()
	config.Address = vaultAddr
	suite.vaultClient, err = api.NewClient(config)
	if err != nil {
		return err
	}
	suite.vaultClient.SetToken("test-token")

	// Configure Vault for testing
	err = suite.setupVaultForTesting()
	if err != nil {
		return err
	}

	// Setup test environment
	suite.testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{"../config/crd/bases"},
	}

	cfg, err := suite.testEnv.Start()
	if err != nil {
		return err
	}

	// Setup scheme
	scheme := runtime.NewScheme()
	err = clientgoscheme.AddToScheme(scheme)
	if err != nil {
		return err
	}
	err = appsv1.AddToScheme(scheme)
	if err != nil {
		return err
	}

	// Create k8s client
	suite.k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}

	// Create vault operator client
	suite.vaultOperator, err = vault.NewClient(vaultAddr, "test-role", "kubernetes")
	if err != nil {
		return err
	}

	// Setup reconciler
	suite.reconciler = &controller.DeploymentReconciler{
		Client:      suite.k8sClient,
		Scheme:      scheme,
		Log:         ctrl.Log.WithName("test"),
		VaultClient: suite.vaultOperator,
		ClusterName: "test-cluster",
	}

	return nil
}

func (suite *IntegrationTestSuite) setupVaultForTesting() error {
	// Enable KV v2 secrets engine
	err := suite.vaultClient.Sys().Mount("secret", &api.MountInput{
		Type: "kv-v2",
	})
	if err != nil {
		return err
	}

	// Enable kubernetes auth
	err = suite.vaultClient.Sys().EnableAuth("kubernetes", "kubernetes", "")
	if err != nil {
		return err
	}

	// Configure kubernetes auth (simplified for testing)
	_, err = suite.vaultClient.Logical().Write("auth/kubernetes/config", map[string]interface{}{
		"kubernetes_host":        "https://kubernetes.default.svc",
		"disable_iss_validation": true,
	})
	if err != nil {
		return err
	}

	// Create test policy
	policy := `
path "secret/data/*" {
  capabilities = ["create", "read", "update", "delete"]
}
path "clusters/test-cluster/*" {
  capabilities = ["create", "read", "update", "delete"]
}
`
	err = suite.vaultClient.Sys().PutPolicy("test-policy", policy)
	if err != nil {
		return err
	}

	// Create test role
	_, err = suite.vaultClient.Logical().Write("auth/kubernetes/role/test-role", map[string]interface{}{
		"bound_service_account_names":      "*",
		"bound_service_account_namespaces": "*",
		"policies":                         "test-policy",
		"ttl":                              "1h",
	})

	return err
}

func (suite *IntegrationTestSuite) TestBasicSecretSync(t *testing.T) {
	ctx := context.Background()

	// Create test secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"username": []byte("testuser"),
			"password": []byte("testpass"),
		},
	}
	err := suite.k8sClient.Create(ctx, secret)
	require.NoError(t, err)

	// Create test deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path": "secret/data/test-app",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test",
							Image: "nginx",
							Env: []corev1.EnvVar{
								{
									Name: "USERNAME",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "test-secret",
											},
											Key: "username",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err = suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Reconcile
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify secret was written to Vault
	vaultPath := "clusters/test-cluster/secret/data/test-app"
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	require.NotNil(t, vaultSecret)

	data, ok := vaultSecret.Data["data"].(map[string]interface{})
	require.True(t, ok)

	secretData, ok := data["test-secret"].(map[string]interface{})
	require.True(t, ok)

	assert.Equal(t, "testuser", secretData["username"])
	assert.Equal(t, "testpass", secretData["password"])
}

func (suite *IntegrationTestSuite) TestPreserveOnDelete(t *testing.T) {
	ctx := context.Background()

	// Create deployment with preserve annotation
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "preserve-test",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path":               "secret/data/preserve-test",
				"vault-sync.io/preserve-on-delete": "true",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "preserve-test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "preserve-test"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "nginx"},
					},
				},
			},
		},
	}

	err := suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Reconcile to create vault secret
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "preserve-test",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Write test data to vault
	vaultPath := "clusters/test-cluster/secret/data/preserve-test"
	_, err = suite.vaultClient.Logical().Write(vaultPath, map[string]interface{}{
		"data": map[string]string{
			"preserved": "data",
		},
	})
	require.NoError(t, err)

	// Delete deployment
	err = suite.k8sClient.Delete(ctx, deployment)
	require.NoError(t, err)

	// Trigger deletion reconciliation
	deployment.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "preserve-test",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify secret still exists in Vault
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	require.NotNil(t, vaultSecret, "Secret should be preserved in Vault")
}

func (suite *IntegrationTestSuite) TestCustomSecretConfiguration(t *testing.T) {
	ctx := context.Background()

	// Create test secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"database_url": []byte("postgres://localhost:5432/db"),
			"api_key":      []byte("secret-api-key"),
			"debug":        []byte("false"),
		},
	}
	err := suite.k8sClient.Create(ctx, secret)
	require.NoError(t, err)

	// Create deployment with custom secret configuration
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "custom-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path":    "secret/data/custom-app",
				"vault-sync.io/secrets": `[{"name":"custom-secret","keys":["database_url","api_key"],"prefix":"app_"}]`,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "custom"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "custom"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx"},
					},
				},
			},
		},
	}

	err = suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Reconcile
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "custom-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify custom configuration was applied
	vaultPath := "clusters/test-cluster/secret/data/custom-app"
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	require.NotNil(t, vaultSecret)

	data, ok := vaultSecret.Data["data"].(map[string]interface{})
	require.True(t, ok)

	// Check prefixed keys
	assert.Equal(t, "postgres://localhost:5432/db", data["app_database_url"])
	assert.Equal(t, "secret-api-key", data["app_api_key"])
	// Debug key should not be present (not in config)
	assert.NotContains(t, data, "app_debug")
	assert.NotContains(t, data, "debug")
}

func (suite *IntegrationTestSuite) TestAutoDiscovery(t *testing.T) {
	ctx := context.Background()

	// Create multiple test secrets
	secret1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"host":     []byte("db.example.com"),
			"username": []byte("dbuser"),
			"password": []byte("dbpass"),
		},
	}
	err := suite.k8sClient.Create(ctx, secret1)
	require.NoError(t, err)

	secret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"token": []byte("api-token-value"),
		},
	}
	err = suite.k8sClient.Create(ctx, secret2)
	require.NoError(t, err)

	// Create deployment that references both secrets
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "autodiscovery-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path": "secret/data/autodiscovery-app",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "autodiscovery"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "autodiscovery"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx",
							Env: []corev1.EnvVar{
								{
									Name: "DB_HOST",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "db-secret"},
											Key:                  "host",
										},
									},
								},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{Name: "api-secret"},
									},
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "db-config",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "db-secret",
								},
							},
						},
					},
				},
			},
		},
	}

	err = suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Reconcile
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "autodiscovery-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify all discovered secrets were synced
	vaultPath := "clusters/test-cluster/secret/data/autodiscovery-app"
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	require.NotNil(t, vaultSecret)

	data, ok := vaultSecret.Data["data"].(map[string]interface{})
	require.True(t, ok)

	// Check both secrets are present as nested objects
	dbSecret, ok := data["db-secret"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "db.example.com", dbSecret["host"])
	assert.Equal(t, "dbuser", dbSecret["username"])
	assert.Equal(t, "dbpass", dbSecret["password"])

	apiSecret, ok := data["api-secret"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "api-token-value", apiSecret["token"])
}

func (suite *IntegrationTestSuite) TestMultiClusterPaths(t *testing.T) {
	ctx := context.Background()

	// Create test secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"data": []byte("cluster-specific-data"),
		},
	}
	err := suite.k8sClient.Create(ctx, secret)
	require.NoError(t, err)

	// Create deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path": "secret/data/cluster-app",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "cluster"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "cluster"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx",
							Env: []corev1.EnvVar{
								{
									Name: "DATA",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "cluster-secret"},
											Key:                  "data",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err = suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Reconcile
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "cluster-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify path includes cluster prefix
	vaultPath := "clusters/test-cluster/secret/data/cluster-app"
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	require.NotNil(t, vaultSecret)

	// Verify secret data
	data, ok := vaultSecret.Data["data"].(map[string]interface{})
	require.True(t, ok)

	secretData, ok := data["cluster-secret"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "cluster-specific-data", secretData["data"])
}

func (suite *IntegrationTestSuite) TestVaultAuthFailure(t *testing.T) {
	// Create a reconciler with invalid auth
	invalidReconciler := &controller.DeploymentReconciler{
		Client:      suite.k8sClient,
		Scheme:      suite.reconciler.Scheme,
		Log:         suite.reconciler.Log,
		VaultClient: &vault.Client{}, // Invalid/empty client
		ClusterName: "test-cluster",
	}

	ctx := context.Background()

	// Create deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "auth-failure-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path": "secret/data/auth-failure-app",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "auth-failure"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "auth-failure"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Image: "nginx"},
					},
				},
			},
		},
	}

	err := suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Reconcile should fail gracefully
	_, err = invalidReconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "auth-failure-app",
			Namespace: "default",
		},
	})
	assert.Error(t, err, "Should fail with auth error")
}

func (suite *IntegrationTestSuite) TestSecretNotFound(t *testing.T) {
	ctx := context.Background()

	// Create deployment referencing non-existent secret
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "missing-secret-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path": "secret/data/missing-secret-app",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "missing-secret"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "missing-secret"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx",
							Env: []corev1.EnvVar{
								{
									Name: "SECRET_VALUE",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "non-existent-secret"},
											Key:                  "value",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err := suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Reconcile should fail with helpful error
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "missing-secret-app",
			Namespace: "default",
		},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "non-existent-secret")
	assert.Contains(t, err.Error(), "check if secret generators have run")
}

func (suite *IntegrationTestSuite) TestSecretRotationDetection(t *testing.T) {
	ctx := context.Background()

	// Create initial secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rotation-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password": []byte("initial-password"),
		},
	}
	err := suite.k8sClient.Create(ctx, secret)
	require.NoError(t, err)

	// Create deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rotation-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path": "secret/data/rotation-app",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "rotation"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "rotation"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx",
							Env: []corev1.EnvVar{
								{
									Name: "PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "rotation-secret"},
											Key:                  "password",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err = suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Initial reconcile
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "rotation-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify initial state in Vault
	vaultPath := "clusters/test-cluster/secret/data/rotation-app"
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data := vaultSecret.Data["data"].(map[string]interface{})
	secretData := data["rotation-secret"].(map[string]interface{})
	assert.Equal(t, "initial-password", secretData["password"])

	// Update the secret (simulate rotation)
	secret.Data["password"] = []byte("rotated-password")
	err = suite.k8sClient.Update(ctx, secret)
	require.NoError(t, err)

	// Reconcile again (should detect change)
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "rotation-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify updated state in Vault
	vaultSecret, err = suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data = vaultSecret.Data["data"].(map[string]interface{})
	secretData = data["rotation-secret"].(map[string]interface{})
	assert.Equal(t, "rotated-password", secretData["password"])
}

func (suite *IntegrationTestSuite) TestRotationCheckAnnotation(t *testing.T) {
	ctx := context.Background()

	// Create initial secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "annotation-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password": []byte("initial-password"),
		},
	}
	err := suite.k8sClient.Create(ctx, secret)
	require.NoError(t, err)

	// Create deployment with rotation check disabled
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "annotation-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path":           "secret/data/annotation-app",
				"vault-sync.io/rotation-check": "disabled",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "annotation"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "annotation"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx",
							Env: []corev1.EnvVar{
								{
									Name: "PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "annotation-secret"},
											Key:                  "password",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err = suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Initial reconcile
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "annotation-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify initial state in Vault
	vaultPath := "clusters/test-cluster/secret/data/annotation-app"
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data := vaultSecret.Data["data"].(map[string]interface{})
	secretData := data["annotation-secret"].(map[string]interface{})
	assert.Equal(t, "initial-password", secretData["password"])

	// Update the secret (simulate rotation)
	secret.Data["password"] = []byte("rotated-password")
	err = suite.k8sClient.Update(ctx, secret)
	require.NoError(t, err)

	// Reconcile again (should sync anyway due to disabled rotation check)
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "annotation-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify updated state in Vault (should be updated despite rotation check being disabled)
	vaultSecret, err = suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data = vaultSecret.Data["data"].(map[string]interface{})
	secretData = data["annotation-secret"].(map[string]interface{})
	assert.Equal(t, "rotated-password", secretData["password"])

	// Test enabling rotation check
	err = suite.k8sClient.Get(ctx, types.NamespacedName{Name: "annotation-app", Namespace: "default"}, deployment)
	require.NoError(t, err)

	deployment.Annotations["vault-sync.io/rotation-check"] = "enabled"
	err = suite.k8sClient.Update(ctx, deployment)
	require.NoError(t, err)

	// Update secret again
	secret.Data["password"] = []byte("final-password")
	err = suite.k8sClient.Update(ctx, secret)
	require.NoError(t, err)

	// Reconcile (should detect change and sync)
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "annotation-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify final state in Vault
	vaultSecret, err = suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data = vaultSecret.Data["data"].(map[string]interface{})
	secretData = data["annotation-secret"].(map[string]interface{})
	assert.Equal(t, "final-password", secretData["password"])
}

func (suite *IntegrationTestSuite) TestBatchOperations(t *testing.T) {
	ctx := context.Background()

	// Create multiple secrets for batch testing
	secrets := []string{"batch-secret-1", "batch-secret-2", "batch-secret-3"}
	for i, secretName := range secrets {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: "default",
			},
			Data: map[string][]byte{
				"password": []byte(fmt.Sprintf("batch-password-%d", i+1)),
			},
		}
		err := suite.k8sClient.Create(ctx, secret)
		require.NoError(t, err)
	}

	// Create deployment that uses all secrets
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "batch-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path": "secret/data/batch-app",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "batch"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "batch"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx",
							Env: []corev1.EnvVar{
								{
									Name: "PASSWORD1",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "batch-secret-1"},
											Key:                  "password",
										},
									},
								},
								{
									Name: "PASSWORD2",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "batch-secret-2"},
											Key:                  "password",
										},
									},
								},
								{
									Name: "PASSWORD3",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "batch-secret-3"},
											Key:                  "password",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err := suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Reconcile
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "batch-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify all secrets are in Vault
	vaultPath := "clusters/test-cluster/secret/data/batch-app"
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data := vaultSecret.Data["data"].(map[string]interface{})

	for i, secretName := range secrets {
		secretData := data[secretName].(map[string]interface{})
		assert.Equal(t, fmt.Sprintf("batch-password-%d", i+1), secretData["password"])
	}
}

func (suite *IntegrationTestSuite) TestChaosRecovery(t *testing.T) {
	ctx := context.Background()

	// This is a placeholder for chaos recovery testing
	// In a full implementation, this would test scenarios like:
	// - Vault server restart
	// - Kubernetes API server interruption
	// - Network partitions
	// - Operator pod restarts

	// For now, we'll test a simple scenario: secret recovery after deletion
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "chaos-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password": []byte("chaos-password"),
		},
	}
	err := suite.k8sClient.Create(ctx, secret)
	require.NoError(t, err)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "chaos-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path": "secret/data/chaos-app",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "chaos"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "chaos"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx",
							Env: []corev1.EnvVar{
								{
									Name: "PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "chaos-secret"},
											Key:                  "password",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err = suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Initial reconcile
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "chaos-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify initial state
	vaultPath := "clusters/test-cluster/secret/data/chaos-app"
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data := vaultSecret.Data["data"].(map[string]interface{})
	secretData := data["chaos-secret"].(map[string]interface{})
	assert.Equal(t, "chaos-password", secretData["password"])

	// Simulate chaos: delete the secret from Vault manually
	_, err = suite.vaultClient.Logical().Delete(vaultPath)
	require.NoError(t, err)

	// Reconcile should recreate the secret
	_, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "chaos-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify recovery
	vaultSecret, err = suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data = vaultSecret.Data["data"].(map[string]interface{})
	secretData = data["chaos-secret"].(map[string]interface{})
	assert.Equal(t, "chaos-password", secretData["password"])
}

func (suite *IntegrationTestSuite) TestPeriodicReconciliation(t *testing.T) {
	ctx := context.Background()

	// Create initial secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "periodic-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"password": []byte("periodic-password"),
		},
	}
	err := suite.k8sClient.Create(ctx, secret)
	require.NoError(t, err)

	// Create deployment with periodic reconciliation enabled
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "periodic-app",
			Namespace: "default",
			Annotations: map[string]string{
				"vault-sync.io/path":      "secret/data/periodic-app",
				"vault-sync.io/reconcile": "30s", // Short interval for testing
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "periodic-app",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "periodic-app",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
							Env: []corev1.EnvVar{
								{
									Name: "PASSWORD",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "periodic-secret",
											},
											Key: "password",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err = suite.k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	// Initial reconcile
	result, err := suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "periodic-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify RequeueAfter is set for periodic reconciliation
	assert.Equal(t, 30*time.Second, result.RequeueAfter)

	// Verify initial state in Vault
	vaultPath := "clusters/test-cluster/secret/data/periodic-app"
	vaultSecret, err := suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data := vaultSecret.Data["data"].(map[string]interface{})
	secretData := data["periodic-secret"].(map[string]interface{})
	assert.Equal(t, "periodic-password", secretData["password"])

	// Manually delete the secret from Vault to simulate accidental deletion
	_, err = suite.vaultClient.Logical().Delete(vaultPath)
	require.NoError(t, err)

	// Verify secret is deleted
	vaultSecret, err = suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	assert.Nil(t, vaultSecret) // Should be nil when deleted

	// Reconcile again (periodic reconciliation should restore the secret)
	result, err = suite.reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "periodic-app",
			Namespace: "default",
		},
	})
	require.NoError(t, err)

	// Verify RequeueAfter is still set
	assert.Equal(t, 30*time.Second, result.RequeueAfter)

	// Verify secret is restored in Vault
	vaultSecret, err = suite.vaultClient.Logical().Read(vaultPath)
	require.NoError(t, err)
	data = vaultSecret.Data["data"].(map[string]interface{})
	secretData = data["periodic-secret"].(map[string]interface{})
	assert.Equal(t, "periodic-password", secretData["password"])
}

func (suite *IntegrationTestSuite) TearDownSuite() {
	if suite.testEnv != nil {
		_ = suite.testEnv.Stop()
	}
	if suite.vaultContainer != nil {
		_ = suite.vaultContainer.Terminate(context.Background())
	}
}
