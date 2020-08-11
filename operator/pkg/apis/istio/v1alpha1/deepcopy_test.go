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

package v1alpha1_test

import (
	"io/ioutil"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/operator/pkg/apis/istio"
	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

// This is to verify that certain proto types handle marshal and unmarshal properly
func TestIstioOperatorSpec_DeepCopy(t *testing.T) {
	tests := []struct {
		name string
		iop  string
	}{
		{
			name: "handles Kubernetes quantity types",
			iop:  "testdata/quantity.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := loadResource(t, tt.iop)
			gotSpec := m.Spec.DeepCopy()
			if diff := cmp.Diff(m.Spec, gotSpec, protocmp.Transform()); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func loadResource(t *testing.T, filepath string) v1alpha1.IstioOperator {
	contents, err := ioutil.ReadFile(filepath)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := istio.UnmarshalIstioOperator(string(contents), false)
	if err != nil {
		t.Fatal(err)
	}
	return *resource
}
