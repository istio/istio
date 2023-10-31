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

package bootstrap

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/kubectl/pkg/util/fieldpath"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/bootstrap/option"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/version"
)

func TestParseDownwardApi(t *testing.T) {
	cases := []struct {
		name string
		m    map[string]string
	}{
		{
			"empty",
			map[string]string{},
		},
		{
			"single",
			map[string]string{"foo": "bar"},
		},
		{
			"multi",
			map[string]string{
				"app":               "istio-ingressgateway",
				"chart":             "gateways",
				"heritage":          "Tiller",
				"istio":             "ingressgateway",
				"pod-template-hash": "54756dbcf9",
			},
		},
		{
			"multi line",
			map[string]string{
				"config": `foo: bar
other: setting`,
				"istio": "ingressgateway",
			},
		},
		{
			"weird values",
			map[string]string{
				"foo": `a1_-.as1`,
				"bar": `a=b`,
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Using the function kubernetes actually uses to write this, we do a round trip of
			// map -> file -> map and ensure the input and output are the same
			got, err := ParseDownwardAPI(fieldpath.FormatMap(tt.m))
			if !reflect.DeepEqual(got, tt.m) {
				t.Fatalf("expected %v, got %v with err: %v", tt.m, got, err)
			}
		})
	}
}

func TestGetNodeMetaData(t *testing.T) {
	inputOwner := "test"
	inputWorkloadName := "workload"

	expectOwner := "test"
	expectWorkloadName := "workload"
	expectExitOnZeroActiveConnections := model.StringBool(true)

	t.Setenv(IstioMetaPrefix+"OWNER", inputOwner)
	t.Setenv(IstioMetaPrefix+"WORKLOAD_NAME", inputWorkloadName)

	dir, _ := os.Getwd()
	defer os.Chdir(dir)
	// prepare a pod label file
	tempDir := t.TempDir()
	os.Chdir(tempDir)
	os.MkdirAll("./etc/istio/pod/", os.ModePerm)
	os.WriteFile(constants.PodInfoLabelsPath, []byte(`istio-locality="region.zone.subzone"`), 0o600)

	node, err := GetNodeMetaData(MetadataOptions{
		ID:                          "test",
		Envs:                        os.Environ(),
		ExitOnZeroActiveConnections: true,
	})

	g := NewWithT(t)
	g.Expect(err).Should(BeNil())
	g.Expect(node.Metadata.Owner).To(Equal(expectOwner))
	g.Expect(node.Metadata.WorkloadName).To(Equal(expectWorkloadName))
	g.Expect(node.Metadata.ExitOnZeroActiveConnections).To(Equal(expectExitOnZeroActiveConnections))
	g.Expect(node.RawMetadata["OWNER"]).To(Equal(expectOwner))
	g.Expect(node.RawMetadata["WORKLOAD_NAME"]).To(Equal(expectWorkloadName))
	g.Expect(node.Metadata.Labels[model.LocalityLabel]).To(Equal("region/zone/subzone"))
}

func TestSetIstioVersion(t *testing.T) {
	test.SetForTest(t, &version.Info.Version, "binary")

	testCases := []struct {
		name            string
		meta            *model.BootstrapNodeMetadata
		binaryVersion   string
		expectedVersion string
	}{
		{
			name:            "if IstioVersion is not specified, set it from binary version",
			meta:            &model.BootstrapNodeMetadata{},
			expectedVersion: "binary",
		},
		{
			name: "if IstioVersion is specified, don't set it from binary version",
			meta: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					IstioVersion: "metadata-version",
				},
			},
			expectedVersion: "metadata-version",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ret := SetIstioVersion(tc.meta)
			if ret.IstioVersion != tc.expectedVersion {
				t.Fatalf("SetIstioVersion: expected '%s', got '%s'", tc.expectedVersion, ret.IstioVersion)
			}
		})
	}
}

func TestConvertNodeMetadata(t *testing.T) {
	node := &model.Node{
		ID: "test",
		Metadata: &model.BootstrapNodeMetadata{
			NodeMetadata: model.NodeMetadata{
				ProxyConfig: &model.NodeMetaProxyConfig{
					ClusterName: &v1alpha1.ProxyConfig_ServiceCluster{
						ServiceCluster: "cluster",
					},
				},
			},
			Owner: "real-owner",
		},
		RawMetadata: map[string]any{},
	}
	node.Metadata.Owner = "real-owner"
	node.RawMetadata["OWNER"] = "fake-owner"
	node.RawMetadata["UNKNOWN"] = "new-field"
	node.RawMetadata["A"] = 1
	node.RawMetadata["B"] = map[string]any{"b": 1}

	out := ConvertNodeToXDSNode(node)
	{
		b, err := protomarshal.MarshalProtoNames(out)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}
		// nolint: lll
		want := `{"id":"test","cluster":"cluster","metadata":{"A":1,"B":{"b":1},"OWNER":"real-owner","PROXY_CONFIG":{"serviceCluster":"cluster"},"UNKNOWN":"new-field"}}`
		test.JSONEquals(t, want, string(b))
	}

	node2 := ConvertXDSNodeToNode(out)
	{
		got, err := json.Marshal(node2)
		if err != nil {
			t.Fatalf("failed to marshal: %v", err)
		}
		// nolint: lll
		want := `{"ID":"test","Metadata":{"PROXY_CONFIG":{"serviceCluster":"cluster"},"OWNER":"real-owner"},"RawMetadata":null,"Locality":null}`
		if want != string(got) {
			t.Fatalf("ConvertXDSNodeToNode: got %q, want %q", string(got), want)
		}
	}
}

func TestConvertNodeServiceClusterNaming(t *testing.T) {
	cases := []struct {
		name        string
		proxyCfg    *model.NodeMetaProxyConfig
		labels      map[string]string
		wantCluster string
	}{
		{
			name:        "no cluster name (no labels)",
			proxyCfg:    &model.NodeMetaProxyConfig{},
			wantCluster: "istio-proxy.bar",
		},
		{
			name:        "no cluster name (defaults)",
			proxyCfg:    &model.NodeMetaProxyConfig{},
			labels:      map[string]string{"app": "foo"},
			wantCluster: "foo.bar",
		},
		{
			name: "service cluster",
			proxyCfg: &model.NodeMetaProxyConfig{
				ClusterName: &v1alpha1.ProxyConfig_ServiceCluster{
					ServiceCluster: "foo",
				},
			},
			wantCluster: "foo",
		},
		{
			name: "trace service name (app label and namespace)",
			proxyCfg: &model.NodeMetaProxyConfig{
				ClusterName: &v1alpha1.ProxyConfig_TracingServiceName_{
					TracingServiceName: v1alpha1.ProxyConfig_APP_LABEL_AND_NAMESPACE,
				},
			},
			labels:      map[string]string{"app": "foo"},
			wantCluster: "foo.bar",
		},
		{
			name: "trace service name (canonical name)",
			proxyCfg: &model.NodeMetaProxyConfig{
				ClusterName: &v1alpha1.ProxyConfig_TracingServiceName_{
					TracingServiceName: v1alpha1.ProxyConfig_CANONICAL_NAME_ONLY,
				},
			},
			labels:      map[string]string{"service.istio.io/canonical-name": "foo"},
			wantCluster: "foo",
		},
		{
			name: "trace service name (canonical name and namespace)",
			proxyCfg: &model.NodeMetaProxyConfig{
				ClusterName: &v1alpha1.ProxyConfig_TracingServiceName_{
					TracingServiceName: v1alpha1.ProxyConfig_CANONICAL_NAME_AND_NAMESPACE,
				},
			},
			labels:      map[string]string{"service.istio.io/canonical-name": "foo"},
			wantCluster: "foo.bar",
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			node := &model.Node{
				ID: "test",
				Metadata: &model.BootstrapNodeMetadata{
					NodeMetadata: model.NodeMetadata{
						ProxyConfig: v.proxyCfg,
						Labels:      v.labels,
						Namespace:   "bar",
					},
				},
			}
			out := ConvertNodeToXDSNode(node)
			if got, want := out.Cluster, v.wantCluster; got != want {
				tt.Errorf("ConvertNodeToXDSNode(%#v) => cluster = %s; want %s", node, got, want)
			}
		})
	}
}

func TestGetStatOptions(t *testing.T) {
	cases := []struct {
		name            string
		metadataOptions MetadataOptions
		// TODO(ramaraochavali): Add validation for prefix and tags also.
		wantInclusionSuffixes []string
	}{
		{
			name: "with exit on zero connections enabled",
			metadataOptions: MetadataOptions{
				ID:                          "test",
				Envs:                        os.Environ(),
				ProxyConfig:                 &v1alpha1.ProxyConfig{},
				ExitOnZeroActiveConnections: true,
			},
			wantInclusionSuffixes: []string{"rbac.allowed", "rbac.denied", "shadow_allowed", "shadow_denied", "downstream_cx_active"},
		},
		{
			name: "with exit on zero connections disabled",
			metadataOptions: MetadataOptions{
				ID:                          "test",
				Envs:                        os.Environ(),
				ProxyConfig:                 &v1alpha1.ProxyConfig{},
				ExitOnZeroActiveConnections: false,
			},
			wantInclusionSuffixes: []string{"rbac.allowed", "rbac.denied", "shadow_allowed", "shadow_denied"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(tt *testing.T) {
			node, _ := GetNodeMetaData(tc.metadataOptions)
			options := getStatsOptions(node.Metadata)
			templateParams, _ := option.NewTemplateParams(options...)
			inclusionSuffixes := templateParams["inclusionSuffix"]
			if !reflect.DeepEqual(inclusionSuffixes, tc.wantInclusionSuffixes) {
				tt.Errorf("unexpected inclusion suffixes. want: %v, got: %v", tc.wantInclusionSuffixes, inclusionSuffixes)
			}
		})
	}
}
