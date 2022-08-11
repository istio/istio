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

package util

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSetLabel(t *testing.T) {
	tests := []struct {
		desc      string
		wantLabel string
		wantValue string
		wantErr   error
	}{
		{
			desc:      "AddMapLabelMapValue",
			wantLabel: "foo",
			wantValue: "bar",
			wantErr:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			resource := &unstructured.Unstructured{Object: make(map[string]any)}
			gotErr := SetLabel(resource, tt.wantLabel, tt.wantValue)
			resourceAccessor, _ := meta.Accessor(resource)
			labels := resourceAccessor.GetLabels()
			if gotVal, ok := labels[tt.wantLabel]; !ok || gotVal != tt.wantValue || gotErr != tt.wantErr {
				t.Errorf("%s: ok: %v, got value: %v, want value: %v, got error: %v, want error: %v", tt.desc, ok, gotVal, tt.wantValue, gotErr, tt.wantErr)
			}
		})
	}
}
