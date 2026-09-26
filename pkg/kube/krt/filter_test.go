// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package krt

import (
	"fmt"
	"reflect"
	"testing"

	"istio.io/istio/pkg/util/smallset"
)

func TestFilterNeedsMatching(t *testing.T) {
	// Every field must have an explicit classification, including fields that do
	// not participate in Matches. Reflection makes adding a field without adding
	// coverage fail, even if needsMatching was also left unchanged.
	tests := map[string]struct {
		filters     []filter
		want        bool
		wantForList bool
	}{
		"keys": {
			filters: []filter{
				{keys: smallset.New("key")},
				{keys: smallset.New([]string{}...)}, // Empty but non-nil matches nothing.
			},
			want: true,
		},
		"index": {
			filters: []filter{{index: &indexFilter{indexMatches: func(any) bool { return false }}}},
			want:    true,
		},
		"selects": {
			filters:     []filter{{selects: map[string]string{}}, {selects: map[string]string{"app": "test"}}},
			want:        true,
			wantForList: true,
		},
		"selectsNonEmpty": {
			filters:     []filter{{selectsNonEmpty: map[string]string{}}, {selectsNonEmpty: map[string]string{"app": "test"}}},
			want:        true,
			wantForList: true,
		},
		"labels": {
			filters:     []filter{{labels: map[string]string{}}, {labels: map[string]string{"app": "test"}}},
			want:        true,
			wantForList: true,
		},
		"generic": {
			filters:     []filter{{generic: func(any) bool { return false }}},
			want:        true,
			wantForList: true,
		},
		"suppressChange": {
			// SuppressChange is handled separately from Matches.
			filters: []filter{{suppressChange: func(any, any) bool { return true }}},
		},
	}

	typ := reflect.TypeOf(filter{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if _, ok := tests[name]; !ok {
			t.Errorf("filter.%s has no needsMatching test; classify the field and update needsMatching if necessary", name)
		}
	}

	check := func(t *testing.T, f filter, want, wantForList bool) {
		t.Helper()
		for _, forList := range []bool{false, true} {
			expected := want
			if forList {
				expected = wantForList
			}
			if got := f.needsMatching(forList); got != expected {
				t.Errorf("needsMatching(%v) = %v, want %v", forList, got, expected)
			}
		}
	}
	t.Run("zero", func(t *testing.T) {
		check(t, filter{}, false, false)
	})
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if _, ok := typ.FieldByName(name); !ok {
				t.Fatalf("test names nonexistent filter field %q", name)
			}
			if len(tt.filters) == 0 {
				t.Fatal("field must have at least one non-zero test case")
			}
			for n, f := range tt.filters {
				t.Run(fmt.Sprint(n), func(t *testing.T) {
					// A different active field could hide an omission in needsMatching.
					// IsZero works on unexported fields without unsafe or Interface().
					value := reflect.ValueOf(f)
					for i := 0; i < typ.NumField(); i++ {
						field := typ.Field(i).Name
						if got, want := !value.Field(i).IsZero(), field == name; got != want {
							t.Fatalf("filter.%s non-zero = %v, want %v; each case must set only %s", field, got, want, name)
						}
					}
					check(t, f, tt.want, tt.wantForList)
				})
			}
		})
	}
}
