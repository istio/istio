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
			filters: []filter{{index: &indexFilter{}}},
			want:    true,
		},
		"selects": {
			filters:     []filter{{extra: &filterExtra{selects: map[string]string{}}}, {extra: &filterExtra{selects: map[string]string{"app": "test"}}}},
			want:        true,
			wantForList: true,
		},
		"selectsNonEmpty": {
			filters:     []filter{{extra: &filterExtra{selectsNonEmpty: map[string]string{}}}, {extra: &filterExtra{selectsNonEmpty: map[string]string{"app": "test"}}}},
			want:        true,
			wantForList: true,
		},
		"labels": {
			filters:     []filter{{extra: &filterExtra{labels: map[string]string{}}}, {extra: &filterExtra{labels: map[string]string{"app": "test"}}}},
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
			filters: []filter{{extra: &filterExtra{suppressChange: func(any, any) bool { return true }}}},
		},
	}

	// filterExtra's fields are classified like filter's own; extra itself is only a container for them.
	nonZeroFields := func(f filter) map[string]bool {
		res := map[string]bool{}
		v := reflect.ValueOf(f)
		for i := 0; i < v.NumField(); i++ {
			if name := v.Type().Field(i).Name; name != "extra" {
				res[name] = !v.Field(i).IsZero()
			}
		}
		x := reflect.ValueOf(filterExtra{})
		if f.extra != nil {
			x = reflect.ValueOf(*f.extra)
		}
		for i := 0; i < x.NumField(); i++ {
			res[x.Type().Field(i).Name] = !x.Field(i).IsZero()
		}
		return res
	}
	allFields := nonZeroFields(filter{})
	for name := range allFields {
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
			if _, ok := allFields[name]; !ok {
				t.Fatalf("test names nonexistent filter field %q", name)
			}
			if len(tt.filters) == 0 {
				t.Fatal("field must have at least one non-zero test case")
			}
			for n, f := range tt.filters {
				t.Run(fmt.Sprint(n), func(t *testing.T) {
					// A different active field could hide an omission in needsMatching.
					// IsZero works on unexported fields without unsafe or Interface().
					for field, got := range nonZeroFields(f) {
						if want := field == name; got != want {
							t.Fatalf("filter.%s non-zero = %v, want %v; each case must set only %s", field, got, want, name)
						}
					}
					check(t, f, tt.want, tt.wantForList)
				})
			}
		})
	}
}

type selectorObject map[string]string

func (s selectorObject) GetLabelSelector() map[string]string {
	return s
}

func TestFilterSelectsNilLabels(t *testing.T) {
	cases := []struct {
		name     string
		selector map[string]string
		want     bool
	}{
		{name: "empty selector", selector: nil, want: true},
		{name: "non-empty selector", selector: map[string]string{"app": "a"}, want: false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			h := &dependency{}
			FilterSelects(nil)(h)
			if got := h.filter.Matches(selectorObject(tt.selector), false); got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}
