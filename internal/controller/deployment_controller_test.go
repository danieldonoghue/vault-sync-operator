package controller

import (
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"github.com/go-logr/logr"
)

func TestExtractSecretNamesFromPodTemplate(t *testing.T) {
	r := &DeploymentReconciler{}

	// Create a test pod template with various secret references
	podTemplate := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "main-container",
					Env: []corev1.EnvVar{
						{
							Name: "DB_PASSWORD",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "database-secret",
									},
									Key: "password",
								},
							},
						},
					},
					EnvFrom: []corev1.EnvFromSource{
						{
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "api-secrets",
								},
							},
						},
					},
				},
			},
			InitContainers: []corev1.Container{
				{
					Name: "init-container",
					Env: []corev1.EnvVar{
						{
							Name: "INIT_TOKEN",
							ValueFrom: &corev1.EnvVarSource{
								SecretKeyRef: &corev1.SecretKeySelector{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "init-secret",
									},
									Key: "token",
								},
							},
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "config-volume",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "config-secret",
						},
					},
				},
			},
		},
	}

	secretNames := r.extractSecretNamesFromPodTemplate(podTemplate)

	// Expected secrets
	expected := map[string]bool{
		"database-secret": true,
		"api-secrets":     true,
		"init-secret":     true,
		"config-secret":   true,
	}

	if len(secretNames) != len(expected) {
		t.Errorf("Expected %d secrets, got %d", len(expected), len(secretNames))
	}

	for expectedSecret := range expected {
		if !secretNames[expectedSecret] {
			t.Errorf("Expected secret %s not found", expectedSecret)
		}
	}

	for foundSecret := range secretNames {
		if !expected[foundSecret] {
			t.Errorf("Unexpected secret %s found", foundSecret)
		}
	}
}

func TestVaultSyncDetection(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name: "vault sync enabled with path",
			annotations: map[string]string{
				VaultPathAnnotation: "secret/data/my-app",
			},
			expected: true,
		},
		{
			name: "vault sync disabled - no path",
			annotations: map[string]string{
				"other-annotation": "value",
			},
			expected: false,
		},
		{
			name: "vault sync disabled - empty path",
			annotations: map[string]string{
				VaultPathAnnotation: "",
			},
			expected: false,
		},
		{
			name:        "vault sync disabled - no annotations",
			annotations: nil,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}

			vaultPath, enabled := deployment.Annotations[VaultPathAnnotation]
			isEnabled := enabled && vaultPath != ""

			if isEnabled != tt.expected {
				t.Errorf("Expected enabled=%v, got enabled=%v", tt.expected, isEnabled)
			}
		})
	}
}

func TestGetReconcileInterval(t *testing.T) {
	// Create a test reconciler with a no-op logger
	r := &DeploymentReconciler{
		Log: logr.Discard(),
	}

	tests := []struct {
		name        string
		annotations map[string]string
		expected    time.Duration
	}{
		{
			name:        "no annotation - disabled by default",
			annotations: nil,
			expected:    0,
		},
		{
			name: "empty annotation - disabled",
			annotations: map[string]string{
				VaultReconcileAnnotation: "",
			},
			expected: 0,
		},
		{
			name: "off annotation - disabled",
			annotations: map[string]string{
				VaultReconcileAnnotation: "off",
			},
			expected: 0,
		},
		{
			name: "valid 5 minute interval",
			annotations: map[string]string{
				VaultReconcileAnnotation: "5m",
			},
			expected: 5 * time.Minute,
		},
		{
			name: "valid 1 hour interval",
			annotations: map[string]string{
				VaultReconcileAnnotation: "1h",
			},
			expected: 1 * time.Hour,
		},
		{
			name: "valid 2 minute interval",
			annotations: map[string]string{
				VaultReconcileAnnotation: "2m",
			},
			expected: 2 * time.Minute,
		},
		{
			name: "too short interval - enforced minimum",
			annotations: map[string]string{
				VaultReconcileAnnotation: "10s",
			},
			expected: 30 * time.Second, // Should be enforced to minimum
		},
		{
			name: "minimum interval - accepted",
			annotations: map[string]string{
				VaultReconcileAnnotation: "30s",
			},
			expected: 30 * time.Second,
		},
		{
			name: "invalid format - disabled",
			annotations: map[string]string{
				VaultReconcileAnnotation: "invalid",
			},
			expected: 0,
		},
		{
			name: "invalid number - disabled",
			annotations: map[string]string{
				VaultReconcileAnnotation: "not-a-duration",
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-deployment",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}

			result := r.getReconcileInterval(deployment)
			
			if result != tt.expected {
				t.Errorf("Expected interval %v, got %v", tt.expected, result)
			}
		})
	}
}
