/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"testing"
)

func TestContainsEqualFoldSubstring(t *testing.T) {
	tests := []struct {
		name     string
		str      string
		substr   string
		expected bool
	}{
		{
			name:     "both lowercase - match",
			str:      "this is a test string",
			substr:   "test",
			expected: true,
		},
		{
			name:     "str uppercase - match",
			str:      "THIS IS A TEST STRING",
			substr:   "test",
			expected: true,
		},
		{
			name:     "substr uppercase - match",
			str:      "this is a test string",
			substr:   "TEST",
			expected: true,
		},
		{
			name:     "mixed case - match",
			str:      "This IS a TEST String",
			substr:   "TeSt",
			expected: true,
		},
		{
			name:     "not found",
			str:      "this is a test string",
			substr:   "banana",
			expected: false,
		},
		{
			name:     "empty string",
			str:      "",
			substr:   "test",
			expected: false,
		},
		{
			name:     "empty substring",
			str:      "this is a test string",
			substr:   "",
			expected: true, // Empty substring is always found in any string - strings.Contains behavior
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ContainsEqualFoldSubstring(tc.str, tc.substr)
			if result != tc.expected {
				t.Errorf("ContainsEqualFoldSubstring(%q, %q) = %v, expected %v", tc.str, tc.substr, result, tc.expected)
			}
		})
	}
}
