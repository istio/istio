// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGetVisibleNamespacesFromExportToAnno(t *testing.T) {
	tests := []struct {
		Annotation        string
		ResourceNamespace string
		Want              []string
	}{
		{"", "ns", []string{"*"}},
		{"ns", "ns", []string{"ns"}},
		{".", "ns", []string{"ns"}},
		{"ns1,ns2", "ns1", []string{"ns1", "ns2"}},
		{"ns1, ns2", "ns1", []string{"ns1", "ns2"}},
		{"ns1 ,ns2", "ns1", []string{"ns1", "ns2"}},
	}
	for _, test := range tests {
		got := getVisibleNamespacesFromExportToAnno(test.Annotation, test.ResourceNamespace)
		if !cmp.Equal(got, test.Want) {
			t.Errorf("getVisibleNamespacesFromExportToAnno(%q, %q) = %#v, but want %#v", test.Annotation, test.ResourceNamespace, got, test.Want)
		}
	}
}
