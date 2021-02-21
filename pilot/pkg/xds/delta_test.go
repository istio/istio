package xds

import (
	"reflect"
	"testing"
)

func TestGenerateDiff(t *testing.T) {
	testCases := []struct{
		desc string
		new []string
		old []string
		expected []string
	}{
		{
			desc: "simple",
			new: []string{"foo", "bar"},
			old: []string{"foo", "bar", "baz"},
			expected: []string{"baz"},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.desc, func(t *testing.T) {
			if got := generateDiff(tt.new, tt.old); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Expected %+v got %+v", tt.expected, got)
			}
		})
	}
}
