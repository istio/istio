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
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/atomic"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubeVersion "k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8s "sigs.k8s.io/gateway-api/apis/v1"
	k8sbeta "sigs.k8s.io/gateway-api/apis/v1beta1"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	istioio_networking_v1beta1 "istio.io/api/networking/v1beta1"
	istio_type_v1beta1 "istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/revisions"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	copyLabelsAnnotationsEnabled  = true
	copyLabelsAnnotationsDisabled = false
)

func TestConfigureIstioGateway(t *testing.T) {
	discoveryNamespacesFilter := buildFilter("default")
	defaultNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	customClass := &k8sbeta.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "custom",
		},
		Spec: k8s.GatewayClassSpec{
			ControllerName: k8s.GatewayController(features.ManagedGatewayController),
		},
	}
	upgradeDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-upgrade",
			Namespace: "default",
			Labels: map[string]string{
				"gateway.istio.io/managed": "istio.io-mesh-controller",
				"istio.io/gateway-name":    "test-upgrade",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"gateway.networking.k8s.io/gateway-name": "test-upgrade",
					"istio.io/gateway-name":                  "test-upgrade",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"gateway.istio.io/managed":               "istio.io-mesh-controller",
						"gateway.networking.k8s.io/gateway-name": "test-upgrade",
						"istio.io/gateway-name":                  "test-upgrade",
						"istio.io/dataplane-mode":                "none",
						"service.istio.io/canonical-name":        "test-upgrade",
						"service.istio.io/canonical-revision":    "latest",
						"sidecar.istio.io/inject":                "false",
						"topology.istio.io/network":              "network-1",
					},
				},
			},
		},
	}

	defaultObjects := []runtime.Object{defaultNamespace}
	store := model.NewFakeStore()
	if _, err := store.Create(config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.ProxyConfig,
			Name:             "test",
			Namespace:        "default",
		},
		Spec: &istioio_networking_v1beta1.ProxyConfig{
			Selector: &istio_type_v1beta1.WorkloadSelector{
				MatchLabels: map[string]string{
					label.IoK8sNetworkingGatewayGatewayName.Name: "default",
				},
			},
			Image: &istioio_networking_v1beta1.ProxyImage{
				ImageType: "distroless",
			},
		},
	}); err != nil {
		t.Fatalf("failed to create ProxyConfigs: %s", err)
	}
	proxyConfig := model.GetProxyConfigs(store, mesh.DefaultMeshConfig())
	tests := []struct {
		name                     string
		gw                       k8sbeta.Gateway
		objects                  []runtime.Object
		pcs                      *model.ProxyConfigs
		values                   string
		discoveryNamespaceFilter kubetypes.DynamicObjectFilter
		ignore                   bool
		copyLabelsAnnotations    *bool
	}{
		{
			name: "simple",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Labels:      map[string]string{"should": "see"},
					Annotations: map[string]string{"should": "see"},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
				},
			},
			objects:                  defaultObjects,
			discoveryNamespaceFilter: discoveryNamespacesFilter,
		},
		{
			name: "simple",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
				},
			},
			objects:                  defaultObjects,
			discoveryNamespaceFilter: buildFilter("not-default"),
			ignore:                   true,
		},
		{
			name: "manual-sa",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Annotations: map[string]string{annotation.GatewayServiceAccount.Name: "custom-sa"},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
				},
			},
			objects:                  defaultObjects,
			discoveryNamespaceFilter: discoveryNamespacesFilter,
		},
		{
			name: "manual-ip",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Annotations: map[string]string{annotation.GatewayNameOverride.Name: "default"},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					Addresses: []k8s.GatewaySpecAddress{{
						Type:  func() *k8s.AddressType { x := k8s.IPAddressType; return &x }(),
						Value: "1.2.3.4",
					}},
				},
			},
			objects:                  defaultObjects,
			discoveryNamespaceFilter: discoveryNamespacesFilter,
		},
		{
			name: "cluster-ip",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					Annotations: map[string]string{
						"networking.istio.io/service-type":  string(corev1.ServiceTypeClusterIP),
						annotation.GatewayNameOverride.Name: "default",
					},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					Listeners: []k8s.Listener{{
						Name:     "http",
						Port:     k8s.PortNumber(80),
						Protocol: k8s.HTTPProtocolType,
					}},
				},
			},
			objects:                  defaultObjects,
			discoveryNamespaceFilter: discoveryNamespacesFilter,
		},
		{
			name: "multinetwork",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Labels:      map[string]string{label.TopologyNetwork.Name: "network-1"},
					Annotations: map[string]string{annotation.GatewayNameOverride.Name: "default"},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					Listeners: []k8s.Listener{{
						Name:     "http",
						Port:     k8s.PortNumber(80),
						Protocol: k8s.HTTPProtocolType,
					}},
				},
			},
			objects:                  defaultObjects,
			discoveryNamespaceFilter: discoveryNamespacesFilter,
		},
		{
			name: "waypoint",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "namespace",
					Namespace: "default",
					Labels: map[string]string{
						label.TopologyNetwork.Name: "network-1", // explicitly set network won't be overwritten
					},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: constants.WaypointGatewayClassName,
					Listeners: []k8s.Listener{{
						Name:     "mesh",
						Port:     k8s.PortNumber(15008),
						Protocol: "ALL",
					}},
				},
			},
			objects: defaultObjects,
			values: `global:
  hub: test
  tag: test
  network: network-2`,
		},
		{
			name: "waypoint-no-network-label",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "namespace",
					Namespace: "default",
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: constants.WaypointGatewayClassName,
					Listeners: []k8s.Listener{{
						Name:     "mesh",
						Port:     k8s.PortNumber(15008),
						Protocol: "ALL",
					}},
				},
			},
			objects: defaultObjects,
			values: `global:
  hub: test
  tag: test
  network: network-1`,
		},
		{
			name: "istio-east-west",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "eastwestgateway",
					Namespace: "istio-system",
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: constants.EastWestGatewayClassName,
					Listeners: []k8s.Listener{{
						Name:     "mesh",
						Port:     k8s.PortNumber(15008),
						Protocol: "ALL",
						TLS: &k8s.GatewayTLSConfig{
							Mode: ptr.Of(k8s.TLSModeTerminate),
							Options: map[k8s.AnnotationKey]k8s.AnnotationValue{
								gatewayTLSTerminateModeKey: "ISTIO_MUTUAL",
							},
						},
					}},
				},
			},
			objects: defaultObjects,
			values: `global:
  hub: test
  tag: test
  network: network-1`,
		},
		{
			name: "proxy-config-crd",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
				},
			},
			objects: defaultObjects,
			pcs:     proxyConfig,
		},
		{
			name: "custom-class",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(customClass.Name),
				},
			},
			objects: defaultObjects,
		},
		{
			name: "infrastructure-labels-annotations",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Labels:      map[string]string{"should-not": "see"},
					Annotations: map[string]string{"should-not": "see"},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					Infrastructure: &k8s.GatewayInfrastructure{
						Labels:      map[k8s.LabelKey]k8s.LabelValue{"foo": "bar", "gateway.networking.k8s.io/ignore": "true"},
						Annotations: map[k8s.AnnotationKey]k8s.AnnotationValue{"fizz": "buzz", "gateway.networking.k8s.io/ignore": "true"},
					},
				},
			},
			objects: defaultObjects,
		},
		{
			name: "kube-gateway-ambient-redirect",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
					// TODO why are we setting this on gateways?
					Labels: map[string]string{
						label.IoIstioDataplaneMode.Name: constants.DataplaneModeAmbient,
					},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
				},
			},
			objects: defaultObjects,
		},
		{
			name: "kube-gateway-ambient-redirect-infra",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: "default",
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					Infrastructure: &k8s.GatewayInfrastructure{
						Labels: map[k8s.LabelKey]k8s.LabelValue{
							k8s.LabelKey(label.IoIstioDataplaneMode.Name): constants.DataplaneModeAmbient,
						},
					},
				},
			},
			objects: defaultObjects,
		},
		{
			name: "istio-upgrade-to-1.24",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-upgrade",
					Namespace: "default",
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: constants.WaypointGatewayClassName,
					Listeners: []k8s.Listener{{
						Name:     "mesh",
						Port:     k8s.PortNumber(15008),
						Protocol: "ALL",
					}},
				},
			},
			objects: defaultObjects,
			values: `global:
  hub: test
  tag: test
  network: network-1`,
		},
		{
			name: "customizations",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "namespace",
					Namespace: "default",
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					Infrastructure: &k8s.GatewayInfrastructure{
						Labels: map[k8s.LabelKey]k8s.LabelValue{"foo": "bar"},
						ParametersRef: &k8s.LocalParametersReference{
							Group: "",
							Kind:  "ConfigMap",
							Name:  "gw-options",
						},
					},
				},
			},
			objects: append(slices.Clone(defaultObjects), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "gw-options", Namespace: "default"},
				Data: map[string]string{
					"podDisruptionBudget": `
spec:
  minAvailable: 1`,
					"horizontalPodAutoscaler": `
spec:
  minReplicas: 2
  maxReplicas: 2`,
					"deployment": `
metadata:
  annotations:
    cm-annotation: cm-annotation-value
spec:
  replicas: 4
  template:
    spec:
      containers:
      - name: istio-proxy
        resources:
          requests:
            cpu: 222m`,
				},
			}),
			values: ``,
		},
		{
			name: "illegal_customizations",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "namespace",
					Namespace: "default",
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					Infrastructure: &k8s.GatewayInfrastructure{
						Labels: map[k8s.LabelKey]k8s.LabelValue{"foo": "bar"},
						ParametersRef: &k8s.LocalParametersReference{
							Group: "",
							Kind:  "ConfigMap",
							Name:  "gw-options",
						},
					},
				},
			},
			objects: append(slices.Clone(defaultObjects), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "gw-options", Namespace: "default"},
				Data: map[string]string{
					"deployment": `
metadata:
  name: not-allowed`,
				},
			}),
			values: ``,
		},
		{
			name: "copy-labels-annotations-disabled-infra-set",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Labels:      map[string]string{"should-not": "see"},
					Annotations: map[string]string{"should-not": "see"},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					Infrastructure: &k8s.GatewayInfrastructure{
						Labels:      map[k8s.LabelKey]k8s.LabelValue{"should": "see-infra-label"},
						Annotations: map[k8s.AnnotationKey]k8s.AnnotationValue{"should": "see-infra-annotation"},
					},
				},
			},
			objects:               defaultObjects,
			copyLabelsAnnotations: &copyLabelsAnnotationsDisabled,
		},
		{
			name: "copy-labels-annotations-disabled-infra-nil",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Labels:      map[string]string{"should-not": "see"},
					Annotations: map[string]string{"should-not": "see"},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
				},
			},
			objects:               defaultObjects,
			copyLabelsAnnotations: &copyLabelsAnnotationsDisabled,
		},
		{
			name: "copy-labels-annotations-enabled-infra-nil",
			gw: k8sbeta.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "default",
					Namespace:   "default",
					Labels:      map[string]string{"should": "see"},
					Annotations: map[string]string{"should": "see"},
				},
				Spec: k8s.GatewaySpec{
					GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
				},
			},
			objects:               defaultObjects,
			copyLabelsAnnotations: &copyLabelsAnnotationsEnabled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.copyLabelsAnnotations != nil {
				test.SetForTest(t, &features.EnableGatewayAPICopyLabelsAnnotations, *tt.copyLabelsAnnotations)
			}
			buf := &bytes.Buffer{}
			client := kube.NewFakeClient(tt.objects...)
			kube.SetObjectFilter(client, tt.discoveryNamespaceFilter)
			client.Kube().Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &kubeVersion.Info{Major: "1", Minor: "28"}
			kclient.NewWriteClient[*k8sbeta.GatewayClass](client).Create(customClass)
			kclient.NewWriteClient[*k8sbeta.Gateway](client).Create(tt.gw.DeepCopy())
			kclient.NewWriteClient[*appsv1.Deployment](client).Create(upgradeDeployment)
			stop := test.NewStop(t)
			env := model.NewEnvironment()
			env.PushContext().ProxyConfigs = tt.pcs
			tw := revisions.NewTagWatcher(client, "", "istio-system")
			go tw.Run(stop)
			d := NewDeploymentController(client, cluster.ID(features.ClusterName), env, testInjectionConfig(t, tt.values), func(fn func()) {
			}, tw, "", "")
			d.patcher = func(gvr schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
				b, err := yaml.JSONToYAML(data)
				if err != nil {
					return err
				}
				buf.Write(b)
				buf.WriteString("---\n")
				return nil
			}
			client.RunAndWait(stop)
			go d.Run(stop)
			kube.WaitForCacheSync("test", stop, d.queue.HasSynced)

			if tt.ignore {
				assert.Equal(t, buf.String(), "")
			} else {
				resp := timestampRegex.ReplaceAll(buf.Bytes(), []byte("lastTransitionTime: fake"))
				if util.Refresh() {
					if err := os.WriteFile(filepath.Join("testdata", "deployment", tt.name+".yaml"), resp, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				util.CompareContent(t, resp, filepath.Join("testdata", "deployment", tt.name+".yaml"))
			}
			// ensure we didn't mutate the object
			if !tt.ignore {
				assert.Equal(t, d.gateways.Get(tt.gw.Name, tt.gw.Namespace), &tt.gw)
			}
		})
	}
}

func buildFilter(allowedNamespace string) kubetypes.DynamicObjectFilter {
	return kubetypes.NewStaticObjectFilter(func(obj any) bool {
		if ns, ok := obj.(string); ok {
			return ns == allowedNamespace
		}
		object := controllers.ExtractObject(obj)
		if object == nil {
			return false
		}
		ns := object.GetNamespace()
		if _, ok := object.(*corev1.Namespace); ok {
			ns = object.GetName()
		}
		return ns == allowedNamespace
	})
}

func TestVersionManagement(t *testing.T) {
	log.SetOutputLevel(istiolog.DebugLevel)
	writes := make(chan string, 10)
	c := kube.NewFakeClient(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
	})
	tw := revisions.NewTagWatcher(c, "default", "istio-system")
	env := &model.Environment{}
	d := NewDeploymentController(c, "", env, testInjectionConfig(t, ""), func(fn func()) {}, tw, "", "")
	reconciles := atomic.NewInt32(0)
	wantReconcile := int32(0)
	expectReconciled := func() {
		t.Helper()
		wantReconcile++
		assert.EventuallyEqual(t, reconciles.Load, wantReconcile, retry.Timeout(time.Second*5), retry.Message("no reconciliation"))
	}

	d.patcher = func(g schema.GroupVersionResource, name string, namespace string, data []byte, subresources ...string) error {
		if g == gvr.Service {
			reconciles.Inc()
		}
		if g == gvr.KubernetesGateway {
			b, err := yaml.JSONToYAML(data)
			if err != nil {
				return err
			}
			writes <- string(b)
		}
		return nil
	}
	stop := test.NewStop(t)
	gws := clienttest.Wrap(t, d.gateways)
	go tw.Run(stop)
	go d.Run(stop)
	c.RunAndWait(stop)
	kube.WaitForCacheSync("test", stop, d.queue.HasSynced)
	// Create a gateway, we should mark our ownership
	defaultGateway := &k8sbeta.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gw",
			Namespace: "default",
		},
		Spec: k8s.GatewaySpec{
			GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
		},
	}
	gws.Create(defaultGateway)
	assert.Equal(t, assert.ChannelHasItem(t, writes), buildPatch(ControllerVersion))
	expectReconciled()
	assert.ChannelIsEmpty(t, writes)
	// Test fake doesn't actual do Apply, so manually do this
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(ControllerVersion)}
	gws.Update(defaultGateway)
	expectReconciled()
	// We shouldn't write in response to our write.
	assert.ChannelIsEmpty(t, writes)

	defaultGateway.Annotations["foo"] = "bar"
	gws.Update(defaultGateway)
	expectReconciled()
	// We should not be updating the version, its already set. Setting it introduces a possible race condition
	// since we use SSA so there is no conflict checks.
	assert.ChannelIsEmpty(t, writes)

	// Somehow the annotation is removed - it should be added back
	defaultGateway.Annotations = map[string]string{}
	gws.Update(defaultGateway)
	expectReconciled()
	assert.Equal(t, assert.ChannelHasItem(t, writes), buildPatch(ControllerVersion))
	assert.ChannelIsEmpty(t, writes)
	// Test fake doesn't actual do Apply, so manually do this
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(ControllerVersion)}
	gws.Update(defaultGateway)
	expectReconciled()
	// We shouldn't write in response to our write.
	assert.ChannelIsEmpty(t, writes)

	// Somehow the annotation is set to an older version - it should be added back
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(1)}
	gws.Update(defaultGateway)
	expectReconciled()
	assert.Equal(t, assert.ChannelHasItem(t, writes), buildPatch(ControllerVersion))
	assert.ChannelIsEmpty(t, writes)
	// Test fake doesn't actual do Apply, so manually do this
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(ControllerVersion)}
	gws.Update(defaultGateway)
	expectReconciled()
	// We shouldn't write in response to our write.
	assert.ChannelIsEmpty(t, writes)

	// Somehow the annotation is set to an new version - we should do nothing
	defaultGateway.Annotations = map[string]string{ControllerVersionAnnotation: fmt.Sprint(10)}
	gws.Update(defaultGateway)
	assert.ChannelIsEmpty(t, writes)
	// Do not expect reconcile
	assert.Equal(t, reconciles.Load(), wantReconcile)
}

func TestHandlerEnqueueFunction(t *testing.T) {
	log.SetOutputLevel(istiolog.DebugLevel)
	defaultNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	defaultGatewayClass := &k8sbeta.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: features.GatewayAPIDefaultGatewayClass,
		},
		Spec: k8s.GatewayClassSpec{
			ControllerName: k8s.GatewayController(features.ManagedGatewayController),
		},
	}

	defaultObjects := []runtime.Object{defaultNamespace, defaultGatewayClass}
	discoveryNamespaceFilter := buildFilter(defaultNamespace.GetName())

	tests := []struct {
		name       string
		event      controllers.Event
		reconciles int32
		objects    []runtime.Object
	}{
		{
			name: "add event",
			event: controllers.Event{
				Event: controllers.EventAdd,
				New: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "new-add",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see"},
						Annotations: map[string]string{"should": "see"},
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
				},
			},
			reconciles: int32(1),
			objects:    defaultObjects,
		},
		{
			name: "delete event",
			event: controllers.Event{
				Event: controllers.EventDelete,
				Old: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "old-delete",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see"},
						Annotations: map[string]string{"should": "see"},
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
				},
			},
			reconciles: int32(1),
			objects:    defaultObjects,
		},
		{
			name: "update event change annotation",
			event: controllers.Event{
				Event: controllers.EventUpdate,
				New: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "update-1",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see"},
						Annotations: map[string]string{"should": "see-new"},
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
					Status: k8s.GatewayStatus{
						Listeners: []k8s.ListenerStatus{
							{
								AttachedRoutes: 100,
							},
						},
					},
				},
				Old: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "update-1",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see"},
						Annotations: map[string]string{"should": "see"},
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
					Status: k8s.GatewayStatus{
						Listeners: []k8s.ListenerStatus{
							{
								AttachedRoutes: 1,
							},
						},
					},
				},
			},
			reconciles: int32(2),
			objects:    defaultObjects,
		},
		{
			name: "update event change label",
			event: controllers.Event{
				Event: controllers.EventUpdate,
				New: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "update-2",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see-new"},
						Annotations: map[string]string{"should": "see"},
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
					Status: k8s.GatewayStatus{
						Listeners: []k8s.ListenerStatus{
							{
								AttachedRoutes: 100,
							},
						},
					},
				},
				Old: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "update-2",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see"},
						Annotations: map[string]string{"should": "see"},
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
					Status: k8s.GatewayStatus{
						Listeners: []k8s.ListenerStatus{
							{
								AttachedRoutes: 1,
							},
						},
					},
				},
			},
			reconciles: int32(2),
			objects:    defaultObjects,
		},
		{
			name: "update event change gateway spec",
			event: controllers.Event{
				Event: controllers.EventUpdate,
				New: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "update-3",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see"},
						Annotations: map[string]string{"should": "see"},
						Generation:  1,
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
					Status: k8s.GatewayStatus{
						Listeners: []k8s.ListenerStatus{
							{
								AttachedRoutes: 100,
							},
						},
					},
				},
				Old: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "update-3",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see"},
						Annotations: map[string]string{"should": "see"},
						Generation:  0,
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
					Status: k8s.GatewayStatus{
						Listeners: []k8s.ListenerStatus{
							{
								AttachedRoutes: 1,
							},
						},
					},
				},
			},
			reconciles: int32(2),
			objects:    defaultObjects,
		},
		{
			name: "update event no change gateway spec",
			event: controllers.Event{
				Event: controllers.EventUpdate,
				New: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "update-4",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see"},
						Annotations: map[string]string{"should": "see"},
						Generation:  0,
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
					Status: k8s.GatewayStatus{
						Listeners: []k8s.ListenerStatus{
							{
								AttachedRoutes: 100,
							},
						},
					},
				},
				Old: &k8sbeta.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "update-4",
						Namespace:   defaultNamespace.GetName(),
						Labels:      map[string]string{"should": "see"},
						Annotations: map[string]string{"should": "see"},
						Generation:  0,
					},
					Spec: k8s.GatewaySpec{
						GatewayClassName: k8s.ObjectName(features.GatewayAPIDefaultGatewayClass),
					},
					Status: k8s.GatewayStatus{
						Listeners: []k8s.ListenerStatus{
							{
								AttachedRoutes: 1,
							},
						},
					},
				},
			},
			reconciles: int32(1),
			objects:    defaultObjects,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reconciles := atomic.NewInt32(0)
			dummyReconcile := func(types.NamespacedName) error {
				reconciles.Inc()
				return nil
			}

			dummyWebHookInjectFn := func() inject.WebhookConfig {
				return inject.WebhookConfig{}
			}
			stop := test.NewStop(t)

			if tt.event.Event == controllers.EventUpdate || tt.event.Event == controllers.EventDelete {
				tt.objects = append(tt.objects, tt.event.Old)
			}
			client := kube.NewFakeClient(tt.objects...)
			kube.SetObjectFilter(client, discoveryNamespaceFilter)
			client.Kube().Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &kubeVersion.Info{Major: "1", Minor: "28"}

			tw := revisions.NewTagWatcher(client, "", "istio-system")
			env := model.NewEnvironment()
			go tw.Run(stop)

			d := NewDeploymentController(client, cluster.ID(features.ClusterName), env, dummyWebHookInjectFn, func(fn func()) {
			}, tw, "", "")
			d.queue.ShutDownEarly()
			d.queue = controllers.NewQueue("fake gateway queue",
				controllers.WithReconciler(dummyReconcile))
			client.RunAndWait(stop)
			go d.Run(stop)

			switch tt.event.Event {
			case controllers.EventAdd:
				gw := tt.event.New.(*k8sbeta.Gateway)
				kclient.NewWriteClient[*k8sbeta.Gateway](client).Create(gw.DeepCopy())
			case controllers.EventDelete:
				gw := tt.event.Old.(*k8sbeta.Gateway)
				kclient.NewWriteClient[*k8sbeta.Gateway](client).Delete(gw.Name, gw.Namespace)
			case controllers.EventUpdate:
				newGw := tt.event.New.(*k8sbeta.Gateway)
				oldGw := tt.event.Old.(*k8sbeta.Gateway)
				kclient.NewWriteClient[*k8sbeta.Gateway](client).Create(oldGw.DeepCopy())
				kube.WaitForCacheSync("test", stop, d.queue.HasSynced)
				kclient.NewWriteClient[*k8sbeta.Gateway](client).Update(newGw.DeepCopy())
			}
			kube.WaitForCacheSync("test", stop, d.queue.HasSynced)

			assert.EventuallyEqual(t, reconciles.Load, tt.reconciles, retry.Timeout(time.Second*5), retry.Message("reconciliations count check"))
		})
	}
}

func testInjectionConfig(t test.Failer, values string) func() inject.WebhookConfig {
	var vc inject.ValuesConfig
	var err error
	if values != "" {
		vc, err = inject.NewValuesConfig(values)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		vc, err = inject.NewValuesConfig(`
global:
  hub: test
  tag: test`)
		if err != nil {
			t.Fatal(err)
		}

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
	return injConfig
}

func buildPatch(version int) string {
	return fmt.Sprintf(`apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  annotations:
    gateway.istio.io/controller-version: "%d"
`, version)
}
