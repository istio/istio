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
	"sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/file"
	istiolog "istio.io/pkg/log"
)

func TestConfigureIstioGateway(t *testing.T) {
	tests := []struct {
		name string
		gw   v1beta1.Gateway
	}{
		{
			"simple",
			v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: v1alpha2.GatewaySpec{
					GatewayClassName: DefaultClassName,
				},
			},
		},
		{
			"manual-sa",
			v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Annotations: map[string]string{GatewaySAOverride: "custom-sa"},
				},
				Spec: v1alpha2.GatewaySpec{
					GatewayClassName: DefaultClassName,
				},
			},
		},
		{
			"manual-ip",
			v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Annotations: map[string]string{GatewayNameOverride: "default"},
				},
				Spec: v1beta1.GatewaySpec{
					GatewayClassName: DefaultClassName,
					Addresses: []v1beta1.GatewayAddress{{
						Type:  func() *v1beta1.AddressType { x := v1beta1.IPAddressType; return &x }(),
						Value: "1.2.3.4",
					}},
				},
			},
		},
		{
			"cluster-ip",
			v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Annotations: map[string]string{
						"networking.istio.io/service-type": string(corev1.ServiceTypeClusterIP),
						GatewayNameOverride:                "default",
					},
				},
				Spec: v1beta1.GatewaySpec{
					GatewayClassName: DefaultClassName,
					Listeners: []v1beta1.Listener{{
						Name:     "http",
						Port:     v1beta1.PortNumber(80),
						Protocol: v1beta1.HTTPProtocolType,
					}},
				},
			},
		},
		{
			"multinetwork",
			v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Labels:      map[string]string{"topology.istio.io/network": "network-1"},
					Annotations: map[string]string{GatewayNameOverride: "default"},
				},
				Spec: v1beta1.GatewaySpec{
					GatewayClassName: DefaultClassName,
					Listeners: []v1beta1.Listener{{
						Name:     "http",
						Port:     v1beta1.PortNumber(80),
						Protocol: v1beta1.HTTPProtocolType,
					}},
				},
			},
		},
		{
			"waypoint",
			v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "namespace",
					Namespace: "default",
				},
				Spec: v1beta1.GatewaySpec{
					GatewayClassName: constants.WaypointGatewayClassName,
					Listeners: []v1beta1.Listener{{
						Name:     "mesh",
						Port:     v1beta1.PortNumber(15008),
						Protocol: "ALL",
					}},
				},
			},
		},
	}
	vc, err := inject.NewValuesConfig(`
global:
  hub: test
  tag: test`)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, err := inject.ParseTemplates(map[string]string{
		"kube-gateway": file.AsStringOrFail(t, filepath.Join(env.IstioSrc, "manifests/charts/istio-control/istio-discovery/files/kube-gateway.yaml")),
		"waypoint":     file.AsStringOrFail(t, filepath.Join(env.IstioSrc, "manifests/charts/istio-control/istio-discovery/files/waypoint.yaml")),
	})
	if err != nil {
		t.Fatal(err)
	}
	injConfig := func() inject.WebhookConfig {
		return inject.WebhookConfig{
			Templates:  tmpl,
			Values:     vc,
			MeshConfig: mesh.DefaultMeshConfig(),
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			d := &DeploymentController{
				client: kube.NewFakeClient(
					&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default-istio", Namespace: "default"}},
					&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "custom-sa", Namespace: "default"}},
				),
				injectConfig: injConfig,
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
