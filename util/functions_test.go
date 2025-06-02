package util

import (
	"reflect"
	"testing"
)

func TestRemoveDuplicatesString(t *testing.T) {
	testCases := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name: "Mixed String Duplicates",
			input: []string{"apple", "orange", "banana", "apple", "grape",
				"orange", "apple"},
			expected: []string{"apple", "orange", "banana", "grape"},
		},
		{
			name:     "No String Duplicates",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "All String Duplicates",
			input:    []string{"go", "go", "go"},
			expected: []string{"go"},
		},
		{
			name:     "Empty String Slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "Nil String Slice",
			input:    nil,
			expected: []string{},
		},
		{
			name:     "Strings with Empty String",
			input:    []string{"a", "", "b", "", "a"},
			expected: []string{"a", "", "b"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := RemoveDuplicates(tc.input)
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("removeDuplicates(%q) = %q; want %q", tc.input, actual,
					tc.expected)
			}
		})
	}
}

func TestRemoveDuplicatesFloat(t *testing.T) {
	testCases := []struct {
		name     string
		input    []float64
		expected []float64
	}{
		{
			name:     "Mixed Float Duplicates",
			input:    []float64{1.1, 2.2, 1.1, 3.3, 4.4, 2.2, 1.1},
			expected: []float64{1.1, 2.2, 3.3, 4.4},
		},
		{
			name:     "No Float Duplicates",
			input:    []float64{1.0, 2.0, 3.0},
			expected: []float64{1.0, 2.0, 3.0},
		},
		{
			name:     "Empty Float Slice",
			input:    []float64{},
			expected: []float64{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := RemoveDuplicates(tc.input)
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("removeDuplicates(%v) = %v; want %v", tc.input, actual,
					tc.expected)
			}
		})
	}
}
