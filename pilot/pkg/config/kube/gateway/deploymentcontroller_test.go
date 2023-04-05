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
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	"sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
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
				Spec: v1alpha2.GatewaySpec{},
			},
		},
		{
			"manual-ip",
			v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: v1beta1.GatewaySpec{
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
					Name:        "default",
					Namespace:   "default",
					Annotations: map[string]string{"networking.istio.io/service-type": string(corev1.ServiceTypeClusterIP)},
				},
				Spec: v1beta1.GatewaySpec{
					Listeners: []v1beta1.Listener{{
						Name:     "http",
						Port:     v1beta1.PortNumber(80),
						Protocol: v1alpha2.HTTPProtocolType,
					}},
				},
			},
		},
		{
			"multinetwork",
			v1beta1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Labels:    map[string]string{"topology.istio.io/network": "network-1"},
				},
				Spec: v1beta1.GatewaySpec{
					Listeners: []v1beta1.Listener{{
						Name:     "http",
						Port:     v1beta1.PortNumber(80),
						Protocol: v1alpha2.HTTPProtocolType,
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

func TestVersionManagement(t *testing.T) {
	log.SetOutputLevel(istiolog.DebugLevel)
	writes := make(chan string, 10)
	c := kube.NewFakeClient(
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "gw-istio", Namespace: "default"}})
	reconciles := atomic.NewInt32(0)
	wantReconcile := int32(0)
	expectReconciled := func() {
		t.Helper()
		wantReconcile++
		assert.EventuallyEqual(t, reconciles.Load, wantReconcile, retry.Timeout(time.Second), retry.Message("no reconciliation"))
	}
	d := NewDeploymentController(c)
	d.patcher = func(g schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
		if g.Resource == "deployments" {
			reconciles.Inc()
		}
		if g.Resource == "gateways" && len(subresources) == 0 {
			b, err := yaml.JSONToYAML(data)
			if err != nil {
				return err
			}
			writes <- string(b)
		}
		return nil
	}
	stop := test.NewStop(t)
	go d.Run(stop)
	c.RunAndWait(stop)

	gws := c.GatewayAPI().GatewayV1beta1().Gateways("default")
	ctx := context.Background()
	// Create a gateway, we should mark our ownership
	defaultGateway := &v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw",
			Namespace: "default",
		},
		Spec: v1beta1.GatewaySpec{GatewayClassName: DefaultClassName},
	}
	gws.Create(ctx, defaultGateway, metav1.CreateOptions{})
	assert.Equal(t, assert.ChannelHasItem(t, writes), buildPatch(ControllerVersion))
	expectReconciled()
	assert.ChannelIsEmpty(t, writes)

	// Test fake doesn't actual do Apply, so manually do this
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(ControllerVersion)}
	gws.Update(ctx, defaultGateway, metav1.UpdateOptions{})
	// We shouldn't write in response to our write.
	assert.ChannelIsEmpty(t, writes)

	defaultGateway.Annotations["foo"] = "bar"
	gws.Update(ctx, defaultGateway, metav1.UpdateOptions{})
	expectReconciled()
	expectReconciled() // There is a bug in the SSA setup causing us to write twice. Fixed in 1.18+
	// We should not be updating the version, its already set. Setting it introduces a possible race condition
	// since we use SSA so there is no conflict checks.
	assert.ChannelIsEmpty(t, writes)

	// Somehow the annotation is removed - it should be added back
	defaultGateway.Annotations = map[string]string{}
	gws.Update(ctx, defaultGateway, metav1.UpdateOptions{})
	expectReconciled()
	assert.Equal(t, assert.ChannelHasItem(t, writes), buildPatch(ControllerVersion))
	assert.ChannelIsEmpty(t, writes)
	// Test fake doesn't actual do Apply, so manually do this
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(ControllerVersion)}
	gws.Update(ctx, defaultGateway, metav1.UpdateOptions{})
	expectReconciled()
	// We shouldn't write in response to our write.
	assert.ChannelIsEmpty(t, writes)

	// Somehow the annotation is set to an older version - it should be added back
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(0)}
	gws.Update(ctx, defaultGateway, metav1.UpdateOptions{})
	expectReconciled()
	assert.Equal(t, assert.ChannelHasItem(t, writes), buildPatch(ControllerVersion))
	assert.ChannelIsEmpty(t, writes)
	// Test fake doesn't actual do Apply, so manually do this
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(ControllerVersion)}
	gws.Update(ctx, defaultGateway, metav1.UpdateOptions{})
	expectReconciled()
	// We shouldn't write in response to our write.
	assert.ChannelIsEmpty(t, writes)

	// Somehow the annotation is set to an new version - we should do nothing
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(3)}
	gws.Update(ctx, defaultGateway, metav1.UpdateOptions{})
	assert.ChannelIsEmpty(t, writes)
	// Do not expect reconcile
	assert.Equal(t, reconciles.Load(), wantReconcile)
}

func buildPatch(version int) string {
	return fmt.Sprintf(`apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  annotations:
    gateway.istio.io/controller-version: "%d"
`, version)
}
