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
	"os"
	"testing"

	tcppb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
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
						Timeout:       &durationpb.Duration{Nanos: 2000 * 1000},
						FailOpen:      true,
						StatusOnError: "403",
						IncludeRequestBodyInCheck: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationRequestBody{
							MaxRequestBytes:     4096,
							AllowPartialMessage: true,
						},
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
						Service:                         "foo/my-custom-ext-authz.foo.svc.cluster.local",
						Port:                            9000,
						Timeout:                         &durationpb.Duration{Seconds: 10},
						FailOpen:                        true,
						StatusOnError:                   "403",
						PathPrefix:                      "/check",
						IncludeRequestHeadersInCheck:    []string{"x-custom-id", "x-prefix-*", "*-suffix"},
						IncludeHeadersInCheck:           []string{"should-not-include-when-IncludeRequestHeadersInCheck-is-set"},
						IncludeAdditionalHeadersInCheck: map[string]string{"x-header-1": "value-1", "x-header-2": "value-2"},
						IncludeRequestBodyInCheck: &meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationRequestBody{
							MaxRequestBytes:     2048,
							AllowPartialMessage: true,
							PackAsBytes:         true,
						},
						HeadersToUpstreamOnAllow:   []string{"Authorization", "x-prefix-*", "*-suffix"},
						HeadersToDownstreamOnDeny:  []string{"Set-cookie", "x-prefix-*", "*-suffix"},
						HeadersToDownstreamOnAllow: []string{"Set-cookie", "x-prefix-*", "*-suffix"},
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
		version    *model.IstioVersion
		input      string
		want       []string
	}{
		{
			name:  "allow-empty-rule",
			input: "allow-empty-rule-in.yaml",
			want:  []string{"allow-empty-rule-out.yaml"},
		},
		{
			name:  "allow-full-rule",
			input: "allow-full-rule-in.yaml",
			want:  []string{"allow-full-rule-out.yaml"},
		},
		{
			name:  "allow-nil-rule",
			input: "allow-nil-rule-in.yaml",
			want:  []string{"allow-nil-rule-out.yaml"},
		},
		{
			name:  "allow-path",
			input: "allow-path-in.yaml",
			want:  []string{"allow-path-out.yaml"},
		},
		{
			name:  "audit-full-rule",
			input: "audit-full-rule-in.yaml",
			want:  []string{"audit-full-rule-out.yaml"},
		},
		{
			name:       "custom-grpc-provider-no-namespace",
			meshConfig: meshConfigGRPCNoNamespace,
			input:      "custom-simple-http-in.yaml",
			want:       []string{"custom-grpc-provider-no-namespace-out1.yaml", "custom-grpc-provider-no-namespace-out2.yaml"},
		},
		{
			name:       "custom-grpc-provider",
			meshConfig: meshConfigGRPC,
			input:      "custom-simple-http-in.yaml",
			want:       []string{"custom-grpc-provider-out1.yaml", "custom-grpc-provider-out2.yaml"},
		},
		{
			name:       "custom-http-provider",
			meshConfig: meshConfigHTTP,
			input:      "custom-simple-http-in.yaml",
			want:       []string{"custom-http-provider-out1.yaml", "custom-http-provider-out2.yaml"},
		},
		{
			name:       "custom-bad-multiple-providers",
			meshConfig: meshConfigHTTP,
			input:      "custom-bad-multiple-providers-in.yaml",
			want:       []string{"custom-bad-out.yaml"},
		},
		{
			name:       "custom-bad-invalid-config",
			meshConfig: meshConfigInvalid,
			input:      "custom-simple-http-in.yaml",
			want:       []string{"custom-bad-out.yaml"},
		},
		{
			name:  "deny-and-allow",
			input: "deny-and-allow-in.yaml",
			want:  []string{"deny-and-allow-out1.yaml", "deny-and-allow-out2.yaml"},
		},
		{
			name:  "deny-empty-rule",
			input: "deny-empty-rule-in.yaml",
			want:  []string{"deny-empty-rule-out.yaml"},
		},
		{
			name:  "dry-run-allow-and-deny",
			input: "dry-run-allow-and-deny-in.yaml",
			want:  []string{"dry-run-allow-and-deny-out1.yaml", "dry-run-allow-and-deny-out2.yaml"},
		},
		{
			name:  "dry-run-allow",
			input: "dry-run-allow-in.yaml",
			want:  []string{"dry-run-allow-out.yaml"},
		},
		{
			name:  "dry-run-mix",
			input: "dry-run-mix-in.yaml",
			want:  []string{"dry-run-mix-out.yaml"},
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
	}

	baseDir := "http/"
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			option := Option{
				IsCustomBuilder: tc.meshConfig != nil,
				Logger:          &AuthzLogger{},
			}
			push := push(t, baseDir+tc.input, tc.meshConfig)
			proxy := node(tc.version)
			defer option.Logger.Report(proxy)
			policies := push.AuthzPolicies.ListAuthorizationPolicies(proxy.ConfigNamespace, proxy.Labels)
			g := New(tc.tdBundle, push, policies, option)
			if g == nil {
				t.Fatalf("failed to create generator")
			}
			got := g.BuildHTTP()
			verify(t, convertHTTP(got), baseDir, tc.want, false /* forTCP */)
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
			name:  "allow-both-http-tcp",
			input: "allow-both-http-tcp-in.yaml",
			want:  []string{"allow-both-http-tcp-out.yaml"},
		},
		{
			name:  "allow-only-http",
			input: "allow-only-http-in.yaml",
			want:  []string{"allow-only-http-out.yaml"},
		},
		{
			name:  "audit-both-http-tcp",
			input: "audit-both-http-tcp-in.yaml",
			want:  []string{"audit-both-http-tcp-out.yaml"},
		},
		{
			name:       "custom-both-http-tcp",
			meshConfig: meshConfigGRPC,
			input:      "custom-both-http-tcp-in.yaml",
			want:       []string{"custom-both-http-tcp-out1.yaml", "custom-both-http-tcp-out2.yaml"},
		},
		{
			name:       "custom-only-http",
			meshConfig: meshConfigHTTP,
			input:      "custom-only-http-in.yaml",
			want:       []string{},
		},
		{
			name:  "deny-both-http-tcp",
			input: "deny-both-http-tcp-in.yaml",
			want:  []string{"deny-both-http-tcp-out.yaml"},
		},
		{
			name:  "dry-run-mix",
			input: "dry-run-mix-in.yaml",
			want:  []string{"dry-run-mix-out.yaml"},
		},
	}

	baseDir := "tcp/"
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			option := Option{
				IsCustomBuilder: tc.meshConfig != nil,
				Logger:          &AuthzLogger{},
			}
			push := push(t, baseDir+tc.input, tc.meshConfig)
			proxy := node(nil)
			defer option.Logger.Report(proxy)
			policies := push.AuthzPolicies.ListAuthorizationPolicies(proxy.ConfigNamespace, proxy.Labels)
			g := New(tc.tdBundle, push, policies, option)
			if g == nil {
				t.Fatalf("failed to create generator")
			}
			got := g.BuildTCP()
			verify(t, convertTCP(got), baseDir, tc.want, true /* forTCP */)
		})
	}
}

func verify(t *testing.T, gots []proto.Message, baseDir string, wants []string, forTCP bool) {
	t.Helper()

	if len(gots) != len(wants) {
		t.Fatalf("got %d configs but want %d", len(gots), len(wants))
	}
	for i, got := range gots {
		gotYaml, err := protomarshal.ToYAML(got)
		if err != nil {
			t.Fatalf("failed to convert to YAML: %v", err)
		}

		wantFile := basePath + baseDir + wants[i]
		want := yamlConfig(t, wantFile, forTCP)
		wantYaml, err := protomarshal.ToYAML(want)
		if err != nil {
			t.Fatalf("failed to convert to YAML: %v", err)
		}

		util.RefreshGoldenFile(t, []byte(gotYaml), wantFile)
		if err := util.Compare([]byte(gotYaml), []byte(wantYaml)); err != nil {
			t.Error(err)
		}
	}
}

func yamlPolicy(t *testing.T, filename string) *model.AuthorizationPolicies {
	t.Helper()
	data, err := os.ReadFile(filename)
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
	data, err := os.ReadFile(filename)
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
		ConfigStore: store,
	})
	if err != nil {
		t.Fatalf("newAuthzPolicies: %v", err)
	}
	return authzPolicies
}

func push(t *testing.T, input string, mc *meshconfig.MeshConfig) *model.PushContext {
	t.Helper()
	p := &model.PushContext{
		AuthzPolicies: yamlPolicy(t, basePath+input),
		Mesh:          mc,
	}
	p.ServiceIndex.HostnameAndNamespace = map[host.Name]map[string]*model.Service{
		"my-custom-ext-authz.foo.svc.cluster.local": {
			"foo": &model.Service{
				Hostname: "my-custom-ext-authz.foo.svc.cluster.local",
			},
		},
	}
	return p
}

func node(version *model.IstioVersion) *model.Proxy {
	return &model.Proxy{
		ID:              "test-node",
		ConfigNamespace: "foo",
		Labels:          httpbin,
		Metadata: &model.NodeMetadata{
			Labels: httpbin,
		},
		IstioVersion: version,
	}
}
