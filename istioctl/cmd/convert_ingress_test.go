// Copyright Istio Authors.
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

package cmd

import (
	"io"
	"os"
	"testing"

	"istio.io/istio/pilot/test/util"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func TestConvertIngress(t *testing.T) {

	tt := []struct {
		in  []string
		out string
	}{
		// Verify we can convert Kubernetes Istio Ingress
		{in: []string{"myservice-ingress.yaml"},
			out: "myservice-gateway.yaml"},

		// Verify we can merge Ingresses
		{in: []string{"myservice-ingress.yaml", "another-ingress.yaml"},
			out: "merged-gateway.yaml"},
	}

	for _, tc := range tt {
		t.Run(tc.out, func(t *testing.T) {
			readers := make([]io.Reader, 0, len(tc.in))
			for _, filename := range tc.in {
				file, err := os.Open("testdata/ingress/" + filename)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close() // nolint: errcheck
				readers = append(readers, file)
			}

			outFilename := "testdata/v1alpha3/" + tc.out
			out, err := os.Create(outFilename)
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close() // nolint: errcheck

			if err := convertConfigs(readers, out, fake.NewSimpleClientset()); err != nil {
				t.Fatalf("Unexpected error converting configs: %v", err)
			}

			util.CompareYAML(outFilename, t)
		})
	}
}

func TestConvertIngressWithNamedPort(t *testing.T) {
	service := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      "my-service",
			Namespace: "mock-ns",
		},
		Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{
				{
					Name:     "my-svc-port",
					Protocol: "TCP",
					Port:     1234,
					TargetPort: intstr.IntOrString{
						Type:   intstr.Int,
						IntVal: 8080,
					},
				},
			},
			Selector: map[string]string{
				"app": "test-app",
			},
		},
	}

	client := fake.NewSimpleClientset(service)

	var readers []io.Reader
	file, err := os.Open("testdata/ingress/named-port-ingress.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close() // nolint: errcheck
	readers = append(readers, file)

	outFilename := "testdata/v1alpha3/named-port-gateway.yaml"
	out, err := os.Create(outFilename)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close() // nolint: errcheck

	if err := convertConfigs(readers, out, client); err != nil {
		t.Fatalf("Unexpected error converting configs: %v", err)
	}

	util.CompareYAML(outFilename, t)
}
