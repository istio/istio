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

package mesh

import (
	"testing"

	"sigs.k8s.io/yaml"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

func TestGetTagFromIstioOperatorUsingYAML(t *testing.T) {
	tests := []struct {
		name    string
		yamlIOP string
		wantTag string
	}{
		{
			name: "Direct Tag",
			yamlIOP: `apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: install-istio
spec:
  tag: 1.20.0-suffix
`,
			wantTag: "1.20.0",
		},
		{
			name: "Pilot Component Tag",
			yamlIOP: `apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: install-istio
spec:
  components:
    pilot:
      tag: 1.21.0
`,
			wantTag: "1.21.0",
		},
		{
			name: "Values Pilot Image",
			yamlIOP: `apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: install-istio
spec:
  values:
    pilot:
      image: docker.io/pilotImage:1.22.0-suffix
`,
			wantTag: "1.22.0",
		},
		{
			name: "Values Override",
			yamlIOP: `apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: install-istio
spec:
  tag: 1.20.0
  components:
    pilot:
      tag: 1.21.0
  values:
    pilot:
      image: docker.io/pilotImage:1.22.0-suffix
`,
			wantTag: "1.22.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var iop v1alpha1.IstioOperator
			if err := yaml.Unmarshal([]byte(tt.yamlIOP), &iop); err != nil {
				t.Fatalf("Failed to unmarshal YAML: %v", err)
			}

			tag, err := getTagFromIstioOperator(&iop)
			if err != nil {
				t.Errorf("getTagFromIstioOperator() error = %v", err)
			}
			if tag != tt.wantTag {
				t.Errorf("getTagFromIstioOperator() = %v, want %v", tag, tt.wantTag)
			}
		})
	}
}
