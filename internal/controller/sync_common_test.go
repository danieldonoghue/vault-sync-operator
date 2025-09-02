package controller

import (
	"testing"

	ctrl "sigs.k8s.io/controller-runtime"
)

// TestParseSecretVersionsAnnotation tests the ParseSecretVersionsAnnotation function
func TestParseSecretVersionsAnnotation(t *testing.T) {
	log := ctrl.Log.WithName("test")
	resourceName := "test-resource"
	resourceNamespace := "default"

	tests := []struct {
		name           string
		annotationValue string
		expected       map[string]string
	}{
		{
			name:           "empty annotation",
			annotationValue: "",
			expected:       map[string]string{},
		},
		{
			name:           "valid JSON annotation",
			annotationValue: `{"secret1":"v1","secret2":"v2"}`,
			expected:       map[string]string{"secret1": "v1", "secret2": "v2"},
		},
		{
			name:           "invalid JSON annotation",
			annotationValue: `{invalid json}`,
			expected:       map[string]string{},
		},
		{
			name:           "null annotation",
			annotationValue: "null",
			expected:       map[string]string{},
		},
		{
			name:           "empty object",
			annotationValue: "{}",
			expected:       map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseSecretVersionsAnnotation(tt.annotationValue, log, resourceName, resourceNamespace)
			
			if len(result) != len(tt.expected) {
				t.Errorf("ParseSecretVersionsAnnotation() length = %v, expected %v", len(result), len(tt.expected))
				return
			}

			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("ParseSecretVersionsAnnotation()[%s] = %v, expected %v", k, result[k], v)
				}
			}
		})
	}
}

// TestSyncContextDetectSecretChanges tests the DetectSecretChanges method
func TestSyncContextDetectSecretChanges(t *testing.T) {
	syncCtx := &SyncContext{
		Log: ctrl.Log.WithName("test"),
	}

	tests := []struct {
		name            string
		lastVersions    map[string]string
		currentVersions map[string]string
		expected        bool
	}{
		{
			name:            "no previous versions - should be considered a change",
			lastVersions:    map[string]string{},
			currentVersions: map[string]string{"secret1": "v1"},
			expected:        true,
		},
		{
			name:            "same versions - no change",
			lastVersions:    map[string]string{"secret1": "v1", "secret2": "v2"},
			currentVersions: map[string]string{"secret1": "v1", "secret2": "v2"},
			expected:        false,
		},
		{
			name:            "version changed - should detect change",
			lastVersions:    map[string]string{"secret1": "v1", "secret2": "v2"},
			currentVersions: map[string]string{"secret1": "v1", "secret2": "v3"},
			expected:        true,
		},
		{
			name:            "secret added - should detect change",
			lastVersions:    map[string]string{"secret1": "v1"},
			currentVersions: map[string]string{"secret1": "v1", "secret2": "v2"},
			expected:        true,
		},
		{
			name:            "secret removed - should detect change",
			lastVersions:    map[string]string{"secret1": "v1", "secret2": "v2"},
			currentVersions: map[string]string{"secret1": "v1"},
			expected:        true,
		},
		{
			name:            "new secret in current - should detect change",
			lastVersions:    map[string]string{"secret1": "v1"},
			currentVersions: map[string]string{"secret1": "v1", "secret2": "v2"},
			expected:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := syncCtx.DetectSecretChanges(tt.lastVersions, tt.currentVersions)
			if result != tt.expected {
				t.Errorf("DetectSecretChanges() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

// TestSyncContextGetChangedSecrets tests the GetChangedSecrets method
func TestSyncContextGetChangedSecrets(t *testing.T) {
	syncCtx := &SyncContext{
		Log: ctrl.Log.WithName("test"),
	}

	tests := []struct {
		name            string
		lastVersions    map[string]string
		currentVersions map[string]string
		expectedLength  int
		shouldContain   []string
	}{
		{
			name:            "no changes",
			lastVersions:    map[string]string{"secret1": "v1", "secret2": "v2"},
			currentVersions: map[string]string{"secret1": "v1", "secret2": "v2"},
			expectedLength:  0,
			shouldContain:   []string{},
		},
		{
			name:            "version changed",
			lastVersions:    map[string]string{"secret1": "v1", "secret2": "v2"},
			currentVersions: map[string]string{"secret1": "v1", "secret2": "v3"},
			expectedLength:  1,
			shouldContain:   []string{"secret2"},
		},
		{
			name:            "secret added",
			lastVersions:    map[string]string{"secret1": "v1"},
			currentVersions: map[string]string{"secret1": "v1", "secret2": "v2"},
			expectedLength:  1,
			shouldContain:   []string{"secret2"},
		},
		{
			name:            "secret removed",
			lastVersions:    map[string]string{"secret1": "v1", "secret2": "v2"},
			currentVersions: map[string]string{"secret1": "v1"},
			expectedLength:  1,
			shouldContain:   []string{"secret2 (removed)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := syncCtx.GetChangedSecrets(tt.lastVersions, tt.currentVersions)
			if len(result) != tt.expectedLength {
				t.Errorf("GetChangedSecrets() length = %v, expected %v", len(result), tt.expectedLength)
				return
			}

			for _, expected := range tt.shouldContain {
				found := false
				for _, actual := range result {
					if actual == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("GetChangedSecrets() should contain %s, but got %v", expected, result)
				}
			}
		})
	}
}