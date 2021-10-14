package kubernetes

import (
	"testing"
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
