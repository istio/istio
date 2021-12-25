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

package gateway

import (
	"bytes"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/kube"
	istiolog "istio.io/pkg/log"
)

func TestConfigureIstioGateway(t *testing.T) {
	tests := []struct {
		name string
		gw   v1alpha2.Gateway
	}{
		{
			"simple",
			v1alpha2.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: v1alpha2.GatewaySpec{},
			},
		},
		{
			"manual-ip",
			v1alpha2.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: v1alpha2.GatewaySpec{
					Addresses: []v1alpha2.GatewayAddress{{
						Type:  func() *v1alpha2.AddressType { x := v1alpha2.IPAddressType; return &x }(),
						Value: "1.2.3.4",
					}},
				},
			},
		},
		{
			"cluster-ip",
			v1alpha2.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Annotations: map[string]string{"networking.istio.io/service-type": string(corev1.ServiceTypeClusterIP)},
				},
				Spec: v1alpha2.GatewaySpec{
					Listeners: []v1alpha2.Listener{{
						Name: "http",
						Port: v1alpha2.PortNumber(80),
					}},
				},
			},
		},
		{
			"multinetwork",
			v1alpha2.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Labels:    map[string]string{"topology.istio.io/network": "network-1"},
				},
				Spec: v1alpha2.GatewaySpec{
					Listeners: []v1alpha2.Listener{{
						Name: "http",
						Port: v1alpha2.PortNumber(80),
					}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			d := &DeploymentController{
				client:    kube.NewFakeClient(),
				templates: processTemplates(),
				patcher: func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
					b, err := yaml.JSONToYAML(data)
					if err != nil {
						return err
					}
					buf.Write(b)
					buf.Write([]byte("---\n"))
					return nil
				},
			}
			err := d.configureIstioGateway(istiolog.FindScope(istiolog.DefaultScopeName), tt.gw)
			if err != nil {
				t.Fatal(err)
			}

			resp := timestampRegex.ReplaceAll(buf.Bytes(), []byte("lastTransitionTime: fake"))
			util.CompareContent(t, resp, filepath.Join("testdata", "deployment", tt.name+".yaml"))
		})
	}
}
