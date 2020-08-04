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

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/util/protomarshal"

	"github.com/gogo/protobuf/proto"

	tcppb "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

const (
	basePath = "testdata/"
)

var (
	httpbin = labels.Collection{
		map[string]string{
			"app":     "httpbin",
			"version": "v1",
		},
	}
)

func TestGenerator_GenerateHTTP(t *testing.T) {
	testCases := []struct {
		name        string
		tdBundle    trustdomain.Bundle
		isVersion14 bool
		input       string
		want        []string
	}{
		{
			name:        "path14",
			input:       "path14-in.yaml",
			isVersion14: true,
			want:        []string{"path14-out.yaml"},
		},
		{
			name:  "path15",
			input: "path15-in.yaml",
			want:  []string{"path15-out.yaml"},
		},
		{
			name:  "action-both",
			input: "action-both-in.yaml",
			want: []string{
				"action-both-deny-out.yaml",
				"action-both-allow-out.yaml"},
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := New(tc.tdBundle, httpbin, "foo", yamlPolicy(t, basePath+tc.input), !tc.isVersion14)
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
		name     string
		tdBundle trustdomain.Bundle
		input    string
		want     []string
	}{
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := New(tc.tdBundle, httpbin, "foo", yamlPolicy(t, basePath+tc.input), true)
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
	var configs []*model.Config
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

func newAuthzPolicies(t *testing.T, policies []*model.Config) *model.AuthorizationPolicies {
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
