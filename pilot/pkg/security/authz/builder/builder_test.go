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

package builder

import (
	"io/ioutil"
	"testing"

	tcppb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"github.com/gogo/protobuf/proto"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/util/protomarshal"
)

const (
	basePath = "testdata/"
)

var (
	httpbin = map[string]string{
		"app":     "httpbin",
		"version": "v1",
	}
	meshConfigGRPCNoNamespace = &meshconfig.MeshConfig{
		ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "default",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc{
					EnvoyExtAuthzGrpc: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider{
						Service:       "my-custom-ext-authz.foo.svc.cluster.local",
						Port:          9000,
						FailOpen:      true,
						StatusOnError: "403",
					},
				},
			},
		},
	}
	meshConfigGRPC = &meshconfig.MeshConfig{
		ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "default",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc{
					EnvoyExtAuthzGrpc: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider{
						Service:       "foo/my-custom-ext-authz.foo.svc.cluster.local",
						Port:          9000,
						FailOpen:      true,
						StatusOnError: "403",
					},
				},
			},
		},
	}
	meshConfigHTTP = &meshconfig.MeshConfig{
		ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "default",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp{
					EnvoyExtAuthzHttp: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationHttpProvider{
						Service:                   "foo/my-custom-ext-authz.foo.svc.cluster.local",
						Port:                      9000,
						FailOpen:                  true,
						StatusOnError:             "403",
						PathPrefix:                "/check",
						IncludeHeadersInCheck:     []string{"x-custom-id"},
						HeadersToUpstreamOnAllow:  []string{"Authorization"},
						HeadersToDownstreamOnDeny: []string{"Set-cookie"},
					},
				},
			},
		},
	}
	meshConfigInvalid = &meshconfig.MeshConfig{
		ExtensionProviders: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "default",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp{
					EnvoyExtAuthzHttp: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationHttpProvider{
						Service:       "foo/my-custom-ext-authz",
						Port:          999999,
						PathPrefix:    "check",
						StatusOnError: "999",
					},
				},
			},
		},
	}
)

func TestGenerator_GenerateHTTP(t *testing.T) {
	testCases := []struct {
		name       string
		tdBundle   trustdomain.Bundle
		meshConfig *meshconfig.MeshConfig
		input      string
		want       []string
	}{
		{
			name:  "path",
			input: "path-in.yaml",
			want:  []string{"path-out.yaml"},
		},
		{
			name:       "action-custom-grpc-provider-no-namespace",
			meshConfig: meshConfigGRPCNoNamespace,
			input:      "action-custom-in.yaml",
			want:       []string{"action-custom-grpc-provider-out1.yaml", "action-custom-grpc-provider-out2.yaml"},
		},
		{
			name:       "action-custom-grpc-provider",
			meshConfig: meshConfigGRPC,
			input:      "action-custom-in.yaml",
			want:       []string{"action-custom-grpc-provider-out1.yaml", "action-custom-grpc-provider-out2.yaml"},
		},
		{
			name:       "action-custom-http-provider",
			meshConfig: meshConfigHTTP,
			input:      "action-custom-in.yaml",
			want:       []string{"action-custom-http-provider-out1.yaml", "action-custom-http-provider-out2.yaml"},
		},
		{
			name:       "action-custom-bad-multiple-providers",
			meshConfig: meshConfigHTTP,
			input:      "action-custom-bad-multiple-providers-in.yaml",
			want:       []string{"action-custom-bad-out.yaml"},
		},
		{
			name:       "action-custom-bad-invalid-config",
			meshConfig: meshConfigInvalid,
			input:      "action-custom-in.yaml",
			want:       []string{"action-custom-bad-out.yaml"},
		},
		{
			name:  "action-both",
			input: "action-both-in.yaml",
			want: []string{
				"action-both-deny-out.yaml",
				"action-both-allow-out.yaml",
			},
		},
		{
			name:  "all-fields",
			input: "all-fields-in.yaml",
			want:  []string{"all-fields-out.yaml"},
		},
		{
			name:  "allow-all",
			input: "allow-all-in.yaml",
			want:  []string{"allow-all-out.yaml"},
		},
		{
			name:  "allow-none",
			input: "allow-none-in.yaml",
			want:  []string{"allow-none-out.yaml"},
		},
		{
			name:  "deny-all",
			input: "deny-all-in.yaml",
			want:  []string{"deny-all-out.yaml"},
		},
		{
			name:  "multiple-policies",
			input: "multiple-policies-in.yaml",
			want:  []string{"multiple-policies-out.yaml"},
		},
		{
			name:  "single-policy",
			input: "single-policy-in.yaml",
			want:  []string{"single-policy-out.yaml"},
		},
		{
			name:     "trust-domain-one-alias",
			tdBundle: trustdomain.NewBundle("td1", []string{"cluster.local"}),
			input:    "simple-policy-td-aliases-in.yaml",
			want:     []string{"simple-policy-td-aliases-out.yaml"},
		},
		{
			name:     "trust-domain-multiple-aliases",
			tdBundle: trustdomain.NewBundle("td1", []string{"cluster.local", "some-td"}),
			input:    "simple-policy-multiple-td-aliases-in.yaml",
			want:     []string{"simple-policy-multiple-td-aliases-out.yaml"},
		},
		{
			name:     "trust-domain-wildcard-in-principal",
			tdBundle: trustdomain.NewBundle("td1", []string{"foobar"}),
			input:    "simple-policy-principal-with-wildcard-in.yaml",
			want:     []string{"simple-policy-principal-with-wildcard-out.yaml"},
		},
		{
			name:     "trust-domain-aliases-in-source-principal",
			tdBundle: trustdomain.NewBundle("new-td", []string{"old-td", "some-trustdomain"}),
			input:    "td-aliases-source-principal-in.yaml",
			want:     []string{"td-aliases-source-principal-out.yaml"},
		},
		{
			name:  "audit-all",
			input: "audit-all-in.yaml",
			want:  []string{"audit-all-out.yaml"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			option := Option{
				IsCustomBuilder: tc.meshConfig != nil,
				Logger:          &AuthzLogger{},
			}
			in := inputParams(t, tc.input, tc.meshConfig)
			defer option.Logger.Report(in)
			g := New(tc.tdBundle, in, option)
			if g == nil {
				t.Fatalf("failed to create generator")
			}
			got := g.BuildHTTP()
			verify(t, convertHTTP(got), tc.want, false /* forTCP */)
		})
	}
}

func TestGenerator_GenerateTCP(t *testing.T) {
	testCases := []struct {
		name       string
		tdBundle   trustdomain.Bundle
		meshConfig *meshconfig.MeshConfig
		input      string
		want       []string
	}{
		{
			name:       "action-custom-http-provider",
			meshConfig: meshConfigHTTP,
			input:      "action-custom-in.yaml",
			want:       []string{},
		},
		{
			name:       "action-custom-HTTP-for-TCP-filter",
			meshConfig: meshConfigGRPC,
			input:      "action-custom-HTTP-for-TCP-filter-in.yaml",
			want:       []string{"action-custom-HTTP-for-TCP-filter-out1.yaml", "action-custom-HTTP-for-TCP-filter-out2.yaml"},
		},
		{
			name:  "action-allow-HTTP-for-TCP-filter",
			input: "action-allow-HTTP-for-TCP-filter-in.yaml",
			want:  []string{"action-allow-HTTP-for-TCP-filter-out.yaml"},
		},
		{
			name:  "action-deny-HTTP-for-TCP-filter",
			input: "action-deny-HTTP-for-TCP-filter-in.yaml",
			want:  []string{"action-deny-HTTP-for-TCP-filter-out.yaml"},
		},
		{
			name:  "action-audit-HTTP-for-TCP-filter",
			input: "action-audit-HTTP-for-TCP-filter-in.yaml",
			want:  []string{"action-audit-HTTP-for-TCP-filter-out.yaml"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			option := Option{
				IsCustomBuilder: tc.meshConfig != nil,
				Logger:          &AuthzLogger{},
			}
			in := inputParams(t, tc.input, tc.meshConfig)
			defer option.Logger.Report(in)
			g := New(tc.tdBundle, in, option)
			if g == nil {
				t.Fatalf("failed to create generator")
			}
			got := g.BuildTCP()
			verify(t, convertTCP(got), tc.want, true /* forTCP */)
		})
	}
}

func verify(t *testing.T, gots []proto.Message, wants []string, forTCP bool) {
	t.Helper()

	if len(gots) != len(wants) {
		t.Fatalf("got %d configs but want %d", len(gots), len(wants))
	}
	for i, got := range gots {
		gotYaml, err := protomarshal.ToYAML(got)
		if err != nil {
			t.Fatalf("failed to convert to YAML: %v", err)
		}

		wantFile := basePath + wants[i]
		want := yamlConfig(t, wantFile, forTCP)
		wantYaml, err := protomarshal.ToYAML(want)
		if err != nil {
			t.Fatalf("failed to convert to YAML: %v", err)
		}

		util.RefreshGoldenFile([]byte(gotYaml), wantFile, t)
		if err := util.Compare([]byte(gotYaml), []byte(wantYaml)); err != nil {
			t.Error(err)
		}
	}
}

func yamlPolicy(t *testing.T, filename string) *model.AuthorizationPolicies {
	t.Helper()
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read input yaml file: %v", err)
	}
	c, _, err := crd.ParseInputs(string(data))
	if err != nil {
		t.Fatalf("failde to parse CRD: %v", err)
	}
	var configs []*config.Config
	for i := range c {
		configs = append(configs, &c[i])
	}

	return newAuthzPolicies(t, configs)
}

func yamlConfig(t *testing.T, filename string, forTCP bool) proto.Message {
	t.Helper()
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if forTCP {
		out := &tcppb.Filter{}
		if err := protomarshal.ApplyYAML(string(data), out); err != nil {
			t.Fatalf("failed to parse YAML: %v", err)
		}
		return out
	}
	out := &httppb.HttpFilter{}
	if err := protomarshal.ApplyYAML(string(data), out); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return out
}

func convertHTTP(in []*httppb.HttpFilter) []proto.Message {
	ret := make([]proto.Message, len(in))
	for i := range in {
		ret[i] = in[i]
	}
	return ret
}

func convertTCP(in []*tcppb.Filter) []proto.Message {
	ret := make([]proto.Message, len(in))
	for i := range in {
		ret[i] = in[i]
	}
	return ret
}

func newAuthzPolicies(t *testing.T, policies []*config.Config) *model.AuthorizationPolicies {
	store := model.MakeIstioStore(memory.Make(collections.Pilot))
	for _, p := range policies {
		if _, err := store.Create(*p); err != nil {
			t.Fatalf("newAuthzPolicies: %v", err)
		}
	}

	authzPolicies, err := model.GetAuthorizationPolicies(&model.Environment{
		IstioConfigStore: store,
	})
	if err != nil {
		t.Fatalf("newAuthzPolicies: %v", err)
	}
	return authzPolicies
}

func inputParams(t *testing.T, input string, mc *meshconfig.MeshConfig) *plugin.InputParams {
	t.Helper()
	ret := &plugin.InputParams{
		Node: &model.Proxy{
			ID:              "test-node",
			ConfigNamespace: "foo",
			Metadata: &model.NodeMetadata{
				Labels: httpbin,
			},
		},
		Push: &model.PushContext{
			AuthzPolicies: yamlPolicy(t, basePath+input),
			Mesh:          mc,
		},
	}
	ret.Push.ServiceIndex.HostnameAndNamespace = map[host.Name]map[string]*model.Service{
		"my-custom-ext-authz.foo.svc.cluster.local": {
			"foo": &model.Service{
				Hostname: "my-custom-ext-authz.foo.svc.cluster.local",
			},
		},
	}
	return ret
}
