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
package xds_test

import (
	"reflect"
	"testing"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extensionsv3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/sets"
)

func TestECDS(t *testing.T) {
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		ConfigString: mustReadFile(t, "./testdata/ecds.yaml"),
	})

	ads := s.ConnectADS().WithType(v3.ExtensionConfigurationType)
	wantExtensionConfigName := "extension-config"
	md := model.NodeMetadata{
		ClusterID: "Kubernetes",
	}
	res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		Node: &corev3.Node{
			Id:       ads.ID,
			Metadata: md.ToStruct(),
		},
		ResourceNames: []string{wantExtensionConfigName},
	})

	var ec corev3.TypedExtensionConfig
	err := res.Resources[0].UnmarshalTo(&ec)
	if err != nil {
		t.Fatal("Failed to unmarshal extension config", err)
		return
	}
	if ec.Name != wantExtensionConfigName {
		t.Errorf("extension config name got %v want %v", ec.Name, wantExtensionConfigName)
	}
}

func makeDockerCredentials(name, namespace string, data map[string]string, secretType corev1.SecretType) *corev1.Secret {
	bdata := map[string][]byte{}
	for k, v := range data {
		bdata[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: bdata,
		Type: secretType,
	}
}

func makeWasmPlugin(name, namespace, secret string) config.Config {
	spec := &extensions.WasmPlugin{
		Phase: extensions.PluginPhase_AUTHN,
	}
	if secret != "" {
		spec.ImagePullSecret = secret
	}
	return config.Config{
		Meta: config.Meta{
			Name:             name,
			Namespace:        namespace,
			GroupVersionKind: gvk.WasmPlugin,
		},
		Spec: spec,
	}
}

var (
	// Secrets
	defaultPullSecret = makeDockerCredentials("default-pull-secret", "default", map[string]string{
		corev1.DockerConfigJsonKey: "default-docker-credential",
	}, corev1.SecretTypeDockerConfigJson)
	rootPullSecret = makeDockerCredentials("root-pull-secret", "istio-system", map[string]string{
		corev1.DockerConfigJsonKey: "root-docker-credential",
	}, corev1.SecretTypeDockerConfigJson)
	wrongTypeSecret = makeDockerCredentials("wrong-type-pull-secret", "default", map[string]string{
		corev1.DockerConfigJsonKey: "wrong-type-docker-credential",
	}, corev1.SecretTypeTLS)

	wasmPlugin             = makeWasmPlugin("default-plugin", "default", "")
	wasmPluginWithSec      = makeWasmPlugin("default-plugin-with-sec", "default", "default-pull-secret")
	wasmPluginWrongSec     = makeWasmPlugin("default-plugin-wrong-sec", "default", "wrong-secret")
	wasmPluginWrongSecType = makeWasmPlugin("default-plugin-wrong-sec-type", "default", "wrong-type-pull-secret")
	rootWasmPluginWithSec  = makeWasmPlugin("root-plugin", "istio-system", "root-pull-secret")
)

func TestECDSGenerate(t *testing.T) {
	cases := []struct {
		name             string
		proxyNamespace   string
		request          *model.PushRequest
		watchedResources []string
		wantExtensions   sets.Set
		wantSecrets      sets.Set
	}{
		{
			name:             "simple",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin"},
			wantExtensions:   sets.Set{"default.default-plugin": {}},
			wantSecrets:      sets.Set{},
		},
		{
			name:             "simple_with_secret",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.Set{"default.default-plugin-with-sec": {}},
			wantSecrets:      sets.Set{"default-docker-credential": {}},
		},
		{
			name:             "miss_secret",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin-wrong-sec"},
			wantExtensions:   sets.Set{"default.default-plugin-wrong-sec": {}},
			wantSecrets:      sets.Set{},
		},
		{
			name:             "wrong_secret_type",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin-wrong-sec-type"},
			wantExtensions:   sets.Set{"default.default-plugin-wrong-sec-type": {}},
			wantSecrets:      sets.Set{},
		},
		{
			name:             "root_and_default",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"default.default-plugin-with-sec", "istio-system.root-plugin"},
			wantExtensions:   sets.Set{"default.default-plugin-with-sec": {}, "istio-system.root-plugin": {}},
			wantSecrets:      sets.Set{"default-docker-credential": {}, "root-docker-credential": {}},
		},
		{
			name:             "only_root",
			proxyNamespace:   "somenamespace",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"istio-system.root-plugin"},
			wantExtensions:   sets.Set{"istio-system.root-plugin": {}},
			wantSecrets:      sets.Set{"root-docker-credential": {}},
		},
		{
			name:           "no_relevant_config_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full: true,
				ConfigsUpdated: map[model.ConfigKey]struct{}{
					{Kind: kind.AuthorizationPolicy}: {},
				},
			},
			watchedResources: []string{"default.default-plugin-with-sec", "istio-system.root-plugin"},
			wantExtensions:   sets.Set{},
			wantSecrets:      sets.Set{},
		},
		{
			name:           "has_relevant_config_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full: true,
				ConfigsUpdated: map[model.ConfigKey]struct{}{
					{Kind: kind.AuthorizationPolicy}: {},
					{Kind: kind.WasmPlugin}:          {},
				},
			},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.Set{"default.default-plugin-with-sec": {}},
			wantSecrets:      sets.Set{"default-docker-credential": {}},
		},
		{
			name:           "non_relevant_secret_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full: true,
				ConfigsUpdated: map[model.ConfigKey]struct{}{
					{Kind: kind.AuthorizationPolicy}: {},
					{Kind: kind.Secret}:              {},
				},
			},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.Set{},
			wantSecrets:      sets.Set{},
		},
		{
			name:           "relevant_secret_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full: true,
				ConfigsUpdated: map[model.ConfigKey]struct{}{
					{Kind: kind.Secret, Name: "default-pull-secret", Namespace: "default"}: {},
				},
			},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.Set{"default.default-plugin-with-sec": {}},
			wantSecrets:      sets.Set{"default-docker-credential": {}},
		},
		{
			name:           "relevant_secret_update_non_full_push",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full: false,
				ConfigsUpdated: map[model.ConfigKey]struct{}{
					{Kind: kind.Secret, Name: "default-pull-secret", Namespace: "default"}: {},
				},
			},
			watchedResources: []string{"default.default-plugin-with-sec"},
			wantExtensions:   sets.Set{"default.default-plugin-with-sec": {}},
			wantSecrets:      sets.Set{"default-docker-credential": {}},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
				KubernetesObjects: []runtime.Object{defaultPullSecret, rootPullSecret, wrongTypeSecret},
				Configs:           []config.Config{wasmPlugin, wasmPluginWithSec, wasmPluginWrongSec, wasmPluginWrongSecType, rootWasmPluginWithSec},
			})

			gen := s.Discovery.Generators[v3.ExtensionConfigurationType]
			tt.request.Start = time.Now()
			proxy := &model.Proxy{
				VerifiedIdentity: &spiffe.Identity{Namespace: tt.proxyNamespace},
				Type:             model.Router,
				Metadata: &model.NodeMetadata{
					ClusterID: "Kubernetes",
				},
			}
			tt.request.Push = s.PushContext()
			tt.request.Push.Mesh.RootNamespace = "istio-system"
			resources, _, _ := gen.Generate(s.SetupProxy(proxy),
				&model.WatchedResource{ResourceNames: tt.watchedResources}, tt.request)
			gotExtensions := sets.Set{}
			gotSecrets := sets.Set{}
			for _, res := range resources {
				gotExtensions.Insert(res.Name)
				ec := &corev3.TypedExtensionConfig{}
				res.Resource.UnmarshalTo(ec)
				wasm := &extensionsv3.Wasm{}
				ec.TypedConfig.UnmarshalTo(wasm)
				gotsecret := wasm.GetConfig().GetVmConfig().GetEnvironmentVariables().GetKeyValues()[model.WasmSecretEnv]
				if gotsecret != "" {
					gotSecrets.Insert(gotsecret)
				}
			}
			if !reflect.DeepEqual(gotSecrets, tt.wantSecrets) {
				t.Errorf("got secrets %v, want secrets %v", gotSecrets, tt.wantSecrets)
			}
			if !reflect.DeepEqual(gotExtensions, tt.wantExtensions) {
				t.Errorf("got extensions %v, want extensions %v", gotExtensions, tt.wantExtensions)
			}
		})
	}
}
