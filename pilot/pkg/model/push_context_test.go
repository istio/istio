package model

import (
	"reflect"
	"testing"
)

func TestMergeUpdateRequest(t *testing.T) {
	cases := []struct {
		name   string
		left   *UpdateRequest
		right  *UpdateRequest
		merged UpdateRequest
	}{
		{
			"left nil",
			nil,
			&UpdateRequest{true, nil},
			UpdateRequest{true, nil},
		},
		{
			"right nil",
			&UpdateRequest{true, nil},
			nil,
			UpdateRequest{true, nil},
		},
		{
			"simple merge",
			&UpdateRequest{true, map[string]struct{}{"ns1": {}}},
			&UpdateRequest{false, map[string]struct{}{"ns2": {}}},
			UpdateRequest{true, map[string]struct{}{"ns1": {}, "ns2": {}}},
		},
		{
			"incremental merge",
			&UpdateRequest{false, map[string]struct{}{"ns1": {}}},
			&UpdateRequest{false, map[string]struct{}{"ns2": {}}},
			UpdateRequest{false, map[string]struct{}{"ns1": {}, "ns2": {}}},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.left.Merge(tt.right)
			if !reflect.DeepEqual(tt.merged, got) {
				t.Fatalf("expected %v, got %v", tt.merged, got)
			}
		})
	}
}
