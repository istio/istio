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
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"sigs.k8s.io/yaml"

	v1alpha12 "istio.io/api/operator/v1alpha1"
	"istio.io/istio/operator/pkg/apis/istio"
	install "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

// This is to verify that certain proto types handle marshal and unmarshal properly
func TestIstioOperatorSpec_DeepCopy(t *testing.T) {
	x := &v1alpha12.ResourceMetricSource{}
	err := yaml.UnmarshalStrict([]byte("targetAverageValue: 100m"), x)
	t.Log(x)
	if err != nil {
		t.Fatal(err)
	}
	y := &v1alpha12.MetricSpec{}
	err = yaml.UnmarshalStrict([]byte(`
type: Resource
resource:
  targetAverageValue: 100m`), y)
	t.Log(y)
	if err != nil {
		t.Fatal(err)
	}
	z := &v1alpha12.HorizontalPodAutoscalerSpec{}
	err = yaml.UnmarshalStrict([]byte(`metrics:
- type: Resource
  resource:
    targetAverageValue: 100m`), z)
	t.Log(z)
	if err != nil {
		t.Fatal(err)
	}
	fa := &install.IstioOperator{}
	err = yaml.UnmarshalStrict([]byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
  name: example-istiocontrolplane
spec:
  profile: demo
  components:
    pilot:
      k8s:
        hpaSpec:
          scaleTargetRef:
            apiVersion: extensions/v1beta1
            kind: Deployment
            name: istiod
          minReplicas: 1
          maxReplicas: 5
          metrics:
          - type: Resource
            resource:
              name: cpu
              targetAverageValue: 100m
`), fa)
	t.Log(fa)
	if err != nil {
		t.Error(err)
	}
	f := &install.IstioOperator{}
	err = yaml.UnmarshalStrict([]byte(`apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  namespace: istio-system
  name: example-istiocontrolplane
spec:
  profile: demo
  components:
    pilot:
      k8s:
        hpaSpec:
          scaleTargetRef:
            apiVersion: extensions/v1beta1
            kind: Deployment
            name: istiod
          minReplicas: 1
          maxReplicas: 5
          metrics:
          - type: Resource
            resource:
              name: cpu
              targetAverageValue: 100m
`), f)
	t.Log(f)
	if err != nil {
		t.Fatal(err)
	}
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

func loadResource(t *testing.T, filepath string) *install.IstioOperator {
	t.Helper()
	contents, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatal(err)
	}
	resource, err := istio.UnmarshalIstioOperator(string(contents), false)
	if err != nil {
		t.Fatal(err)
	}
	return resource
}
