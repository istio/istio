// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
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
		ClusterID: constants.DefaultClusterName,
	}
	res := ads.RequestResponseAck(t, &discovery.DiscoveryRequest{
		Node: &core.Node{
			Id:       ads.ID,
			Metadata: md.ToStruct(),
		},
		ResourceNames: []string{wantExtensionConfigName},
	})

	var ec core.TypedExtensionConfig
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
		wantExtensions   sets.String
		wantSecrets      sets.String
	}{
		{
			name:             "simple",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin"},
			wantExtensions:   sets.String{"extenstions.istio.io/wasmplugin/default.default-plugin": {}},
			wantSecrets:      sets.String{},
		},
		{
			name:             "simple_with_secret",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec"},
			wantExtensions:   sets.String{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}},
		},
		{
			name:             "miss_secret",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin-wrong-sec"},
			wantExtensions:   sets.String{"extenstions.istio.io/wasmplugin/default.default-plugin-wrong-sec": {}},
			wantSecrets:      sets.String{},
		},
		{
			name:             "wrong_secret_type",
			proxyNamespace:   "default",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin-wrong-sec-type"},
			wantExtensions:   sets.String{"extenstions.istio.io/wasmplugin/default.default-plugin-wrong-sec-type": {}},
			wantSecrets:      sets.String{},
		},
		{
			name:           "root_and_default",
			proxyNamespace: "default",
			request:        &model.PushRequest{Full: true},
			watchedResources: []string{
				"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec",
				"extenstions.istio.io/wasmplugin/istio-system.root-plugin",
			},
			wantExtensions: sets.String{
				"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec": {},
				"extenstions.istio.io/wasmplugin/istio-system.root-plugin":        {},
			},
			wantSecrets: sets.String{"default-docker-credential": {}, "root-docker-credential": {}},
		},
		{
			name:             "only_root",
			proxyNamespace:   "somenamespace",
			request:          &model.PushRequest{Full: true},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/istio-system.root-plugin"},
			wantExtensions:   sets.String{"extenstions.istio.io/wasmplugin/istio-system.root-plugin": {}},
			wantSecrets:      sets.String{"root-docker-credential": {}},
		},
		{
			name:           "no_relevant_config_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.AuthorizationPolicy}),
			},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec", "extenstions.istio.io/wasmplugin/istio-system.root-plugin"},
			wantExtensions:   sets.String{},
			wantSecrets:      sets.String{},
		},
		{
			name:           "has_relevant_config_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.AuthorizationPolicy}, model.ConfigKey{Kind: kind.WasmPlugin}),
			},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec"},
			wantExtensions:   sets.String{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}},
		},
		{
			name:           "non_relevant_secret_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.AuthorizationPolicy}, model.ConfigKey{Kind: kind.Secret}),
			},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec"},
			wantExtensions:   sets.String{},
			wantSecrets:      sets.String{},
		},
		{
			name:           "non_relevant_secret_update_and_wasm_plugin",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.WasmPlugin}, model.ConfigKey{Kind: kind.Secret}),
			},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec"},
			wantExtensions:   sets.String{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}},
		},
		{
			name:           "relevant_secret_update",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           true,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "default-pull-secret", Namespace: "default"}),
			},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec"},
			wantExtensions:   sets.String{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}},
		},
		{
			name:           "relevant_secret_update_non_full_push",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           false,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "default-pull-secret", Namespace: "default"}),
			},
			watchedResources: []string{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec"},
			wantExtensions:   sets.String{"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec": {}},
			wantSecrets:      sets.String{"default-docker-credential": {}},
		},
		// All the credentials should be sent to istio-agent even if one of them is only updated,
		// because `istio-agent` does not keep the credentials.
		{
			name:           "multi_wasmplugin_update_secret",
			proxyNamespace: "default",
			request: &model.PushRequest{
				Full:           false,
				ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Secret, Name: "default-pull-secret", Namespace: "default"}),
			},
			watchedResources: []string{
				"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec",
				"extenstions.istio.io/wasmplugin/istio-system.root-plugin",
			},
			wantExtensions: sets.String{
				"extenstions.istio.io/wasmplugin/default.default-plugin-with-sec": {},
				"extenstions.istio.io/wasmplugin/istio-system.root-plugin":        {},
			},
			wantSecrets: sets.String{"default-docker-credential": {}, "root-docker-credential": {}},
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
					ClusterID: constants.DefaultClusterName,
				},
			}
			tt.request.Push = s.PushContext()
			tt.request.Push.Mesh.RootNamespace = "istio-system"
			resources, _, _ := gen.Generate(s.SetupProxy(proxy),
				&model.WatchedResource{ResourceNames: tt.watchedResources}, tt.request)
			gotExtensions := sets.String{}
			gotSecrets := sets.String{}
			for _, res := range resources {
				gotExtensions.Insert(res.Name)
				ec := &core.TypedExtensionConfig{}
				res.Resource.UnmarshalTo(ec)
				wasm := &wasm.Wasm{}
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
