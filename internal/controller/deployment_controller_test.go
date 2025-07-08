package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
