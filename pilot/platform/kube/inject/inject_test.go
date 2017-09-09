// Copyright 2017 Istio Authors
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

package inject

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/ghodss/yaml"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/proxy"
	"istio.io/pilot/test/util"
	"istio.io/pilot/tools/version"
)

func TestImageName(t *testing.T) {
	want := "docker.io/istio/proxy_init:latest"
	if got := InitImageName("docker.io/istio", "latest", true); got != want {
		t.Errorf("InitImageName() failed: got %q want %q", got, want)
	}
	want = "docker.io/istio/proxy_debug:latest"
	if got := ProxyImageName("docker.io/istio", "latest", true); got != want {
		t.Errorf("ProxyImageName() failed: got %q want %q", got, want)
	}
	want = "docker.io/istio/proxy:latest"
	if got := ProxyImageName("docker.io/istio", "latest", false); got != want {
		t.Errorf("ProxyImageName(debug:false) failed: got %q want %q", got, want)
	}
}

// Tag name should be kept in sync with value in platform/kube/inject/refresh.sh
const unitTestTag = "unittest"

// This is the hub to expect in platform/kube/inject/testdata/frontend.yaml.injected
// and the other .injected "want" YAMLs
const unitTestHub = "docker.io/istio"

// Default unit test DebugMode parameter
const unitTestDebugMode = true

func TestIntoResourceFile(t *testing.T) {
	cases := []struct {
		authConfigPath  string
		configMapName   string
		enableAuth      bool
		in              string
		want            string
		imagePullPolicy string
		enableCoreDump  bool
		debugMode       bool
	}{
		// "testdata/hello.yaml" is tested in http_test.go (with debug)
		{
			in:        "testdata/hello.yaml",
			want:      "testdata/hello.yaml.injected",
			debugMode: true,
		},
		{
			in:   "testdata/hello-probes.yaml",
			want: "testdata/hello-probes.yaml.injected",
		},
		{
			configMapName: "config-map-name",
			in:            "testdata/hello.yaml",
			want:          "testdata/hello-config-map-name.yaml.injected",
		},
		{
			in:   "testdata/frontend.yaml",
			want: "testdata/frontend.yaml.injected",
		},
		{
			in:   "testdata/hello-service.yaml",
			want: "testdata/hello-service.yaml.injected",
		},
		{
			in:   "testdata/hello-multi.yaml",
			want: "testdata/hello-multi.yaml.injected",
		},
		{
			imagePullPolicy: "Always",
			in:              "testdata/hello.yaml",
			want:            "testdata/hello-always.yaml.injected",
		},
		{
			imagePullPolicy: "Never",
			in:              "testdata/hello.yaml",
			want:            "testdata/hello-never.yaml.injected",
		},
		{
			in:   "testdata/hello-ignore.yaml",
			want: "testdata/hello-ignore.yaml.injected",
		},
		{
			in:   "testdata/multi-init.yaml",
			want: "testdata/multi-init.yaml.injected",
		},
		{
			in:   "testdata/statefulset.yaml",
			want: "testdata/statefulset.yaml.injected",
		},
		{
			in:             "testdata/enable-core-dump.yaml",
			want:           "testdata/enable-core-dump.yaml.injected",
			enableCoreDump: true,
		},
		{
			enableAuth:     true,
			authConfigPath: "/etc/certs/",
			in:             "testdata/auth.yaml",
			want:           "testdata/auth.yaml.injected",
		},
		{
			enableAuth:     true,
			authConfigPath: "/etc/certs/",
			in:             "testdata/auth.non-default-service-account.yaml",
			want:           "testdata/auth.non-default-service-account.yaml.injected",
		},
		{
			enableAuth:     true,
			authConfigPath: "/etc/non-default-dir/",
			in:             "testdata/auth.yaml",
			want:           "testdata/auth.cert-dir.yaml.injected",
		},
		{
			in:   "testdata/daemonset.yaml",
			want: "testdata/daemonset.yaml.injected",
		},
		{
			in:   "testdata/job.yaml",
			want: "testdata/job.yaml.injected",
		},
		{
			in:   "testdata/replicaset.yaml",
			want: "testdata/replicaset.yaml.injected",
		},
		{
			in:   "testdata/replicationcontroller.yaml",
			want: "testdata/replicationcontroller.yaml.injected",
		},
	}

	for _, c := range cases {
		mesh := proxy.DefaultMeshConfig()
		if c.enableAuth {
			mesh.AuthPolicy = proxyconfig.MeshConfig_MUTUAL_TLS
		}

		config := &Config{
			Policy:     InjectionPolicyEnabled,
			Namespaces: []string{v1.NamespaceAll},
			Params: Params{
				InitImage:         InitImageName(unitTestHub, unitTestTag, c.debugMode),
				ProxyImage:        ProxyImageName(unitTestHub, unitTestTag, c.debugMode),
				ImagePullPolicy:   "IfNotPresent",
				Verbosity:         DefaultVerbosity,
				SidecarProxyUID:   DefaultSidecarProxyUID,
				Version:           "12345678",
				EnableCoreDump:    c.enableCoreDump,
				Mesh:              &mesh,
				MeshConfigMapName: "istio",
				DebugMode:         c.debugMode,
			},
		}

		if c.configMapName != "" {
			config.Params.MeshConfigMapName = c.configMapName
		}

		if c.imagePullPolicy != "" {
			config.Params.ImagePullPolicy = c.imagePullPolicy
		}

		in, err := os.Open(c.in)
		if err != nil {
			t.Fatalf("Failed to open %q: %v", c.in, err)
		}
		defer func() { _ = in.Close() }()
		var got bytes.Buffer
		if err = IntoResourceFile(config, in, &got); err != nil {
			t.Fatalf("IntoResourceFile(%v) returned an error: %v", c.in, err)
		}

		util.CompareContent(got.Bytes(), c.want, t)
	}
}

func TestInjectRequired(t *testing.T) {
	cases := []struct {
		policy InjectionPolicy
		meta   *metav1.ObjectMeta
		want   bool
	}{
		{
			policy: InjectionPolicyEnabled,
			meta: &metav1.ObjectMeta{
				Name:        "no-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: true,
		},
		{
			policy: InjectionPolicyEnabled,
			meta: &metav1.ObjectMeta{
				Name:      "default-policy",
				Namespace: "test-namespace",
			},
			want: true,
		},
		{
			policy: InjectionPolicyEnabled,
			meta: &metav1.ObjectMeta{
				Name:        "force-on-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{istioSidecarAnnotationPolicyKey: "true"},
			},
			want: true,
		},
		{
			policy: InjectionPolicyEnabled,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{istioSidecarAnnotationPolicyKey: "false"},
			},
			want: false,
		},
		{
			policy: InjectionPolicyDisabled,
			meta: &metav1.ObjectMeta{
				Name:        "no-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{},
			},
			want: false,
		},
		{
			policy: InjectionPolicyDisabled,
			meta: &metav1.ObjectMeta{
				Name:      "default-policy",
				Namespace: "test-namespace",
			},
			want: false,
		},
		{
			policy: InjectionPolicyDisabled,
			meta: &metav1.ObjectMeta{
				Name:        "force-on-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{istioSidecarAnnotationPolicyKey: "true"},
			},
			want: true,
		},
		{
			policy: InjectionPolicyDisabled,
			meta: &metav1.ObjectMeta{
				Name:        "force-off-policy",
				Namespace:   "test-namespace",
				Annotations: map[string]string{istioSidecarAnnotationPolicyKey: "false"},
			},
			want: false,
		},
	}

	for _, c := range cases {
		if got := injectRequired(c.policy, c.meta); got != c.want {
			t.Errorf("injectRequired(%v, %v) got %v want %v", c.policy, c.meta, got, c.want)
		}
	}
}

func TestGetMeshConfig(t *testing.T) {
	_, cl := makeClient(t)
	t.Parallel()
	ns, err := util.CreateNamespace(cl)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(cl, ns)

	cases := []struct {
		name      string
		configMap *v1.ConfigMap
		queryName string
		wantErr   bool
	}{
		{
			name:      "bad query name",
			queryName: "bad-query-name-foo-bar",
			configMap: &v1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "bad-query-name"},
				Data: map[string]string{
					ConfigMapKey: "", // empty config
				},
			},
			wantErr: true,
		},
		{
			name:      "bad config key",
			queryName: "bad-config-key",
			configMap: &v1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "bad-config-key"},
				Data: map[string]string{
					"bad-key": "",
				},
			},
			wantErr: true,
		},
		{
			name:      "bad config data",
			queryName: "bad-config-data",
			configMap: &v1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "bad-config-data"},
				Data: map[string]string{
					ConfigMapKey: "bad config",
				},
			},
			wantErr: true,
		},
		{
			name:      "good",
			queryName: "good",
			configMap: &v1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "good"},
				Data: map[string]string{
					ConfigMapKey: "", // empty config
				},
			},
			wantErr: false,
		},
	}

	want := proxy.DefaultMeshConfig()

	for _, c := range cases {
		_, err = cl.CoreV1().ConfigMaps(ns).Create(c.configMap)
		if err != nil {
			t.Fatalf("%v: Create failed: %v", c.name, err)
		}
		_, got, err := GetMeshConfig(cl, ns, c.queryName)
		gotErr := err != nil
		if gotErr != c.wantErr {
			t.Fatalf("%v: GetMeshConfig returned wrong error value: got %v want %v: err=%v", c.name, gotErr, c.wantErr, err)
		}
		if gotErr {
			continue
		}
		if !reflect.DeepEqual(got, &want) {
			t.Fatalf("%v: GetMeshConfig returned the wrong result: \ngot  %v \nwant %v", c.name, got, &want)
		}
		if err = cl.CoreV1().ConfigMaps(ns).Delete(c.configMap.Name, &metav1.DeleteOptions{}); err != nil {
			t.Fatalf("%v: Delete failed: %v", c.name, err)
		}
	}
}

func TestGetInitializerConfig(t *testing.T) {
	_, cl := makeClient(t)
	t.Parallel()
	ns, err := util.CreateNamespace(cl)
	if err != nil {
		t.Fatal(err.Error())
	}
	defer util.DeleteNamespace(cl, ns)

	goodConfig := Config{
		Policy:          InjectionPolicyDisabled,
		InitializerName: DefaultInitializerName,
		Params: Params{
			InitImage:         InitImageName(unitTestHub, unitTestTag, false),
			ProxyImage:        ProxyImageName(unitTestHub, unitTestTag, false),
			SidecarProxyUID:   1234,
			MeshConfigMapName: "something",
			ImagePullPolicy:   "Always",
		},
	}
	goodConfigYAML, err := yaml.Marshal(&goodConfig)
	if err != nil {
		t.Fatalf("Failed to create test config data: %v", err)
	}

	cases := []struct {
		name      string
		configMap *v1.ConfigMap
		queryName string
		wantErr   bool
		want      Config
	}{
		{
			name:      "bad query name",
			queryName: "bad-query-name-foo-bar",
			configMap: &v1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "bad-query-name"},
			},
			wantErr: true,
		},
		{
			name:      "bad config key",
			queryName: "bad-config-key",
			configMap: &v1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "bad-config-key"},
				Data: map[string]string{
					"bad-key": "",
				},
			},
			wantErr: true,
		},
		{
			name:      "override all defaults",
			queryName: "default-config",
			configMap: &v1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "default-config"},
				Data: map[string]string{
					InitializerConfigMapKey: "",
				},
			},
			want: Config{
				Policy:          DefaultInjectionPolicy,
				InitializerName: DefaultInitializerName,
				Params: Params{
					InitImage:         InitImageName(DefaultHub, version.Info.Version, false),
					ProxyImage:        ProxyImageName(DefaultHub, version.Info.Version, false),
					SidecarProxyUID:   DefaultSidecarProxyUID,
					MeshConfigMapName: DefaultMeshConfigMapName,
					ImagePullPolicy:   DefaultImagePullPolicy,
				},
			},
		},
		{
			name:      "normal config",
			queryName: "config",
			configMap: &v1.ConfigMap{
				TypeMeta:   metav1.TypeMeta{Kind: "ConfigMap", APIVersion: "v1"},
				ObjectMeta: metav1.ObjectMeta{Name: "config"},
				Data: map[string]string{
					InitializerConfigMapKey: string(goodConfigYAML),
				},
			},
			want: goodConfig,
		},
	}

	for _, c := range cases {
		_, err = cl.CoreV1().ConfigMaps(ns).Create(c.configMap)
		if err != nil {
			t.Fatalf("%v: Create failed: %v", c.name, err)
		}
		got, err := GetInitializerConfig(cl, ns, c.queryName)
		gotErr := err != nil
		if gotErr != c.wantErr {
			t.Fatalf("%v: GetMeshConfig returned wrong error value: got %v want %v: err=%v", c.name, gotErr, c.wantErr, err)
		}
		if gotErr {
			continue
		}
		if !reflect.DeepEqual(got, &c.want) {
			t.Fatalf("%v: GetMeshConfig returned the wrong result: \ngot  %v \nwant %v", c.name, got, &c.want)
		}
		if err = cl.CoreV1().ConfigMaps(ns).Delete(c.configMap.Name, &metav1.DeleteOptions{}); err != nil {
			t.Fatalf("%v: Delete failed: %v", c.name, err)
		}
	}
}
