package controller

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/danieldonoghue/vault-sync-operator/internal/vault"
)

var _ = ginkgo.Describe("SecretReconciler", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		k8sClient client.Client
		testEnv   *envtest.Environment
		reconciler *SecretReconciler
	)

	ginkgo.BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())

		logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

		ginkgo.By("bootstrapping test environment")
		testEnv = &envtest.Environment{}

		cfg, err := testEnv.Start()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cfg).NotTo(gomega.BeNil())

		err = corev1.AddToScheme(scheme.Scheme)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(k8sClient).NotTo(gomega.BeNil())

		// Mock VaultClient - in a real test you'd want to mock this properly
		mockVaultClient := &vault.Client{}

		reconciler = &SecretReconciler{
			Client:      k8sClient,
			Scheme:      scheme.Scheme,
			Log:         ctrl.Log.WithName("controllers").WithName("Secret"),
			VaultClient: mockVaultClient,
			ClusterName: "test-cluster",
		}
	})

	ginkgo.AfterEach(func() {
		cancel()
		ginkgo.By("tearing down the test environment")
		err := testEnv.Stop()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	})

	ginkgo.Context("When reconciling a Secret with vault-sync annotations", func() {
		ginkgo.It("Should add finalizer to annotated Secret", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "default",
					Annotations: map[string]string{
						VaultPathAnnotation: "secret/data/test",
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"key1": []byte("value1"),
					"key2": []byte("value2"),
				},
			}

			err := k8sClient.Create(ctx, secret)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Reconcile the Secret
			_, err = reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      secret.Name,
					Namespace: secret.Namespace,
				},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check that finalizer was added
			updatedSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      secret.Name,
				Namespace: secret.Namespace,
			}, updatedSecret)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(updatedSecret.Finalizers).To(gomega.ContainElement(VaultSyncFinalizer))
		})

		ginkgo.It("Should ignore Secret without vault-sync annotations", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-synced-secret",
					Namespace: "default",
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"key1": []byte("value1"),
				},
			}

			err := k8sClient.Create(ctx, secret)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Reconcile the Secret
			result, err := reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      secret.Name,
					Namespace: secret.Namespace,
				},
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(result.Requeue).To(gomega.BeFalse())
			gomega.Expect(result.RequeueAfter).To(gomega.BeZero())

			// Check that no finalizer was added
			updatedSecret := &corev1.Secret{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      secret.Name,
				Namespace: secret.Namespace,
			}, updatedSecret)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(updatedSecret.Finalizers).NotTo(gomega.ContainElement(VaultSyncFinalizer))
		})

		ginkgo.It("Should handle periodic reconciliation annotation", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "periodic-secret",
					Namespace: "default",
					Annotations: map[string]string{
						VaultPathAnnotation:      "secret/data/periodic",
						VaultReconcileAnnotation: "5m",
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"key1": []byte("value1"),
				},
			}

			err := k8sClient.Create(ctx, secret)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Add finalizer first (simulating previous reconciliation)
			secret.Finalizers = []string{VaultSyncFinalizer}
			err = k8sClient.Update(ctx, secret)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Test the getReconcileInterval method
			interval := reconciler.getReconcileInterval(secret)
			gomega.Expect(interval).To(gomega.Equal(5 * time.Minute))
		})

		ginkgo.It("Should enforce minimum reconciliation interval", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "short-interval-secret",
					Namespace: "default",
					Annotations: map[string]string{
						VaultPathAnnotation:      "secret/data/short",
						VaultReconcileAnnotation: "10s", // Less than 30s minimum
					},
				},
				Type: corev1.SecretTypeOpaque,
			}

			// Test the getReconcileInterval method
			interval := reconciler.getReconcileInterval(secret)
			gomega.Expect(interval).To(gomega.Equal(30 * time.Second)) // Should be enforced to minimum
		})

		ginkgo.It("Should handle rotation check disabled annotation", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-rotation-check-secret",
					Namespace: "default",
					Annotations: map[string]string{
						VaultPathAnnotation:          "secret/data/no-rotation",
						VaultRotationCheckAnnotation: "disabled",
					},
				},
				Type: corev1.SecretTypeOpaque,
			}

			// Test the isRotationCheckDisabled method
			disabled := reconciler.isRotationCheckDisabled(secret)
			gomega.Expect(disabled).To(gomega.BeTrue())
		})
	})
})

func TestSecretReconciler(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "SecretReconciler Suite")
}