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
	"os"
	"reflect"
	"regexp"
	"testing"

	. "github.com/onsi/gomega"
	"k8s.io/kubectl/pkg/util/fieldpath"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/bootstrap/option"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/test"
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

func TestRequiredEnvoyStatsMatcherInclusionRegexes(t *testing.T) {
	ok, _ := regexp.MatchString(requiredEnvoyStatsMatcherInclusionRegexes, "vhost.default.local:18000.route.routev1.upstream_rq_200")
	if !ok {
		t.Fatal("requiredEnvoyStatsMatcherInclusionRegexes doesn't match the route's stat_prefix")
	}
}

func TestServiceClusterOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		metadata *model.BootstrapNodeMetadata
		expected string
	}{
		{
			name:  "non-empty name that is not istio-proxy",
			input: "my-service",
			metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Namespace: "default",
					Labels:    map[string]string{"app": "test"},
				},
			},
			expected: "my-service",
		},
		{
			name:  "empty name with app.kubernetes.io/name label",
			input: "",
			metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name": "k8s-app",
						"app":                    "legacy-app",
					},
				},
			},
			expected: "k8s-app.default",
		},
		{
			name:  "empty name with only app label",
			input: "",
			metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Namespace: "default",
					Labels:    map[string]string{"app": "legacy-app"},
				},
			},
			expected: "legacy-app.default",
		},
		{
			name:  "empty name with canonical service label",
			input: "",
			metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Namespace: "default",
					Labels: map[string]string{
						model.IstioCanonicalServiceLabelName: "canonical-name",
						"app.kubernetes.io/name":             "k8s-app",
						"app":                                "legacy-app",
					},
				},
			},
			expected: "canonical-name.default",
		},
		{
			name:  "istio-proxy name falls back to labels",
			input: "istio-proxy",
			metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Namespace: "default",
					Labels:    map[string]string{"app.kubernetes.io/name": "k8s-app"},
				},
			},
			expected: "k8s-app.default",
		},
		{
			name:  "no labels falls back to workload name",
			input: "",
			metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Namespace:    "default",
					WorkloadName: "my-workload",
				},
			},
			expected: "my-workload.default",
		},
		{
			name:  "no labels and no workload name falls back to istio-proxy",
			input: "",
			metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Namespace: "default",
				},
			},
			expected: "istio-proxy.default",
		},
		{
			name:  "no namespace returns just istio-proxy",
			input: "",
			metadata: &model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{},
			},
			expected: "istio-proxy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := serviceClusterOrDefault(tt.input, tt.metadata)
			if result != tt.expected {
				t.Errorf("serviceClusterOrDefault() = %v, want %v", result, tt.expected)
			}
		})
	}
}
