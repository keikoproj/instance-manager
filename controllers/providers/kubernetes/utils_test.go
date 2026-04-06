package kubernetes

import (
	"errors"
	"testing"

	"github.com/keikoproj/instance-manager/controllers/common"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

func TestHasAnnotation(t *testing.T) {
	annotations := map[string]string{
		"foo": "bar",
		"baz": "",
	}
	tests := []struct {
		name        string
		annotations map[string]string
		key         string
		expected    bool
	}{
		{
			name:        "present with value",
			annotations: annotations,
			key:         "foo",
			expected:    true,
		},
		{
			name:        "present with no value",
			annotations: annotations,
			key:         "baz",
			expected:    true,
		},
		{
			name:        "absent",
			annotations: annotations,
			key:         "missing",
			expected:    false,
		},
	}

	for _, tc := range tests {
		result := HasAnnotation(tc.annotations, tc.key)
		if result != tc.expected {
			t.Fail()
		}
	}
}

func TestHasAnnotationWithValue(t *testing.T) {
	annotations := map[string]string{
		"foo": "bar",
		"baz": "",
	}
	tests := []struct {
		name        string
		annotations map[string]string
		key         string
		value       string
		expected    bool
	}{
		{
			name:        "present with value expecting value",
			annotations: annotations,
			key:         "foo",
			value:       "bar",
			expected:    true,
		},
		{
			name:        "present with value expecting no value",
			annotations: annotations,
			key:         "foo",
			value:       "",
			expected:    false,
		},
		{
			name:        "present with no value expecting no value",
			annotations: annotations,
			key:         "baz",
			value:       "",
			expected:    true,
		},
		{
			name:        "present with no value expecting value",
			annotations: annotations,
			key:         "baz",
			value:       "boop",
			expected:    false,
		},
		{
			name:        "absent",
			annotations: annotations,
			key:         "missing",
			value:       "",
			expected:    false,
		},
	}

	for _, tc := range tests {
		result := HasAnnotationWithValue(tc.annotations, tc.key, tc.value)
		if result != tc.expected {
			t.Fatalf("Unexpected result %v. expected %v from %s", result, tc.expected, tc.name)
		}
	}
}

// Test IsStorageError
func TestIsStorageError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "storage error",
			err:      errors.New("StorageError: invalid object"),
			expected: true,
		},
		{
			name:     "storage error different case",
			err:      errors.New("sToRaGeErRor: invalid object"),
			expected: true,
		},
		{
			name:     "not a storage error",
			err:      errors.New("some other error"),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsStorageError(tc.err)
			if result != tc.expected {
				t.Errorf("IsStorageError(%v) = %v, expected %v", tc.err, result, tc.expected)
			}
		})
	}
}

// Test IsPathValue
func TestIsPathValue(t *testing.T) {
	// Create a test unstructured resource
	resource := unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"foo": "bar",
			},
		},
	}

	tests := []struct {
		name     string
		resource unstructured.Unstructured
		path     string
		value    string
		expected bool
	}{
		{
			name:     "path exists with matching value",
			resource: resource,
			path:     "spec.foo",
			value:    "bar",
			expected: true,
		},
		{
			name:     "path exists with matching value different case",
			resource: resource,
			path:     "spec.foo",
			value:    "BAR",
			expected: true,
		},
		{
			name:     "path exists with non-matching value",
			resource: resource,
			path:     "spec.foo",
			value:    "baz",
			expected: false,
		},
		{
			name:     "path does not exist",
			resource: resource,
			path:     "spec.missing",
			value:    "any",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsPathValue(tc.resource, tc.path, tc.value)
			if result != tc.expected {
				t.Errorf("IsPathValue() = %v, expected %v", result, tc.expected)
			}
		})
	}
}

// Test ObjectDigest
func TestObjectDigest(t *testing.T) {
	testObj := map[string]string{"foo": "bar"}

	tests := []struct {
		name     string
		obj      interface{}
		expected string
	}{
		{
			name:     "nil object",
			obj:      nil,
			expected: "N/A",
		},
		{
			name:     "non-nil object",
			obj:      testObj,
			expected: common.StringMD5("map[foo:bar]"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ObjectDigest(tc.obj)
			if tc.expected != result {
				t.Errorf("ObjectDigest(%v) = %v, expected %v", tc.obj, result, tc.expected)
			}
		})
	}
}

// Test NewStatusPatch
func TestNewStatusPatch(t *testing.T) {
	patch := NewStatusPatch()

	if patch == nil {
		t.Error("NewStatusPatch() returned nil")
		return
	}

	// Basic validation that the patch was created with empty spec
	if patch.from.Spec.Provisioner != "" {
		t.Errorf("Expected empty provisioner in statusPatch, got %s", patch.from.Spec.Provisioner)
	}

	// Verify the patch type is MergePatchType
	patchType := patch.Type()
	if patchType != types.MergePatchType {
		t.Errorf("Expected patch type types.MergePatchType, got %v", patchType)
	}
}
