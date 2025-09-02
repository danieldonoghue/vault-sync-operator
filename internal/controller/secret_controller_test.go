package controller

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TestSecretReconcilerGetReconcileInterval tests the getReconcileInterval method
func TestSecretReconcilerGetReconcileInterval(t *testing.T) {
	reconciler := &SecretReconciler{
		Log: ctrl.Log.WithName("test"),
	}

	tests := []struct {
		name        string
		annotations map[string]string
		expected    time.Duration
	}{
		{
			name:        "no annotation - disabled by default",
			annotations: map[string]string{},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-secret",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}

			result := reconciler.getReconcileInterval(secret)
			if result != tt.expected {
				t.Errorf("getReconcileInterval() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestSecretReconcilerIsRotationCheckDisabled tests the isRotationCheckDisabled method
func TestSecretReconcilerIsRotationCheckDisabled(t *testing.T) {
	reconciler := &SecretReconciler{
		Log: ctrl.Log.WithName("test"),
	}

	tests := []struct {
		name        string
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "no annotation - enabled by default",
			annotations: map[string]string{},
			expected:    false,
		},
		{
			name: "disabled annotation",
			annotations: map[string]string{
				VaultRotationCheckAnnotation: "disabled",
			},
			expected: true,
		},
		{
			name: "enabled annotation",
			annotations: map[string]string{
				VaultRotationCheckAnnotation: "enabled",
			},
			expected: false,
		},
		{
			name: "other value - not disabled",
			annotations: map[string]string{
				VaultRotationCheckAnnotation: "some-other-value",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-secret",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}

			result := reconciler.isRotationCheckDisabled(secret)
			if result != tt.expected {
				t.Errorf("isRotationCheckDisabled() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestSecretReconcilerGetLastKnownSecretVersions tests the getLastKnownSecretVersions method
func TestSecretReconcilerGetLastKnownSecretVersions(t *testing.T) {
	reconciler := &SecretReconciler{
		Log: ctrl.Log.WithName("test"),
	}

	tests := []struct {
		name        string
		annotations map[string]string
		expected    map[string]string
	}{
		{
			name:        "no annotation",
			annotations: map[string]string{},
			expected:    map[string]string{},
		},
		{
			name: "empty annotation",
			annotations: map[string]string{
				VaultSecretVersionsAnnotation: "",
			},
			expected: map[string]string{},
		},
		{
			name: "valid JSON annotation",
			annotations: map[string]string{
				VaultSecretVersionsAnnotation: `{"secret1":"v1","secret2":"v2"}`,
			},
			expected: map[string]string{"secret1": "v1", "secret2": "v2"},
		},
		{
			name: "invalid JSON annotation",
			annotations: map[string]string{
				VaultSecretVersionsAnnotation: `{invalid json}`,
			},
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-secret",
					Namespace:   "default",
					Annotations: tt.annotations,
				},
			}

			result := reconciler.getLastKnownSecretVersions(secret)
			if len(result) != len(tt.expected) {
				t.Errorf("getLastKnownSecretVersions() length = %v, expected %v", len(result), len(tt.expected))
				return
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("getLastKnownSecretVersions()[%s] = %v, expected %v", k, result[k], v)
				}
			}
		})
	}
}