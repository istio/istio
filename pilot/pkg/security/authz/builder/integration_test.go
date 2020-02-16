// Copyright 2019 Istio Authors
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
	"reflect"
	"testing"

	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestBuildHTTPFilter(t *testing.T) {
	testCases := []struct {
		name              string
		trustDomainBundle trustdomain.Bundle
		policies          *model.AuthorizationPolicies
		forTCPFilter      bool
		wantDeny          *http_config.RBAC
		wantAllow         *http_config.RBAC
	}{
		{
			name:         "v1beta1 action allow with HTTP fields for TCP filter",
			forTCPFilter: true,
			policies:     getPolicies("testdata/v1beta1/action-allow-HTTP-for-TCP-filter-in.yaml", t),
			wantAllow:    getProto("testdata/v1beta1/action-allow-HTTP-for-TCP-filter-out.yaml", t),
		},
		{
			name:      "v1beta1 action both",
			policies:  getPolicies("testdata/v1beta1/action-both-in.yaml", t),
			wantDeny:  getProto("testdata/v1beta1/action-both-deny-out.yaml", t),
			wantAllow: getProto("testdata/v1beta1/action-both-allow-out.yaml", t),
		},
		{
			name:         "v1beta1 action deny with HTTP fields for TCP filter",
			forTCPFilter: true,
			policies:     getPolicies("testdata/v1beta1/action-deny-HTTP-for-TCP-filter-in.yaml", t),
			wantDeny:     getProto("testdata/v1beta1/action-deny-HTTP-for-TCP-filter-out.yaml", t),
		},
		{
			name:      "v1beta1 all fields",
			policies:  getPolicies("testdata/v1beta1/all-fields-in.yaml", t),
			wantAllow: getProto("testdata/v1beta1/all-fields-out.yaml", t),
		},
		{
			name:      "v1beta1 allow all",
			policies:  getPolicies("testdata/v1beta1/allow-all-in.yaml", t),
			wantAllow: getProto("testdata/v1beta1/allow-all-out.yaml", t),
		},
		{
			name:      "v1beta1 deny all",
			policies:  getPolicies("testdata/v1beta1/deny-all-in.yaml", t),
			wantAllow: getProto("testdata/v1beta1/deny-all-out.yaml", t),
		},
		{
			name:      "v1beta1 multiple policies",
			policies:  getPolicies("testdata/v1beta1/multiple-policies-in.yaml", t),
			wantAllow: getProto("testdata/v1beta1/multiple-policies-out.yaml", t),
		},
		{
			name:      "v1beta1 not override v1alpha1",
			policies:  getPolicies("testdata/v1beta1/not-override-v1alpha1-in.yaml", t),
			wantAllow: getProto("testdata/v1beta1/not-override-v1alpha1-out.yaml", t),
		},
		{
			name:      "v1beta1 override v1alpha1",
			policies:  getPolicies("testdata/v1beta1/override-v1alpha1-in.yaml", t),
			wantAllow: getProto("testdata/v1beta1/override-v1alpha1-out.yaml", t),
		},
		{
			name:      "v1beta1 single policy",
			policies:  getPolicies("testdata/v1beta1/single-policy-in.yaml", t),
			wantAllow: getProto("testdata/v1beta1/single-policy-out.yaml", t),
		},
		{
			name:      "v1alpha1 all fields",
			policies:  getPolicies("testdata/v1alpha1/all-fields-in.yaml", t),
			wantAllow: getProto("testdata/v1alpha1/all-fields-out.yaml", t),
		},
		{
			name:              "v1beta1 one trust domain alias",
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td1", []string{"cluster.local"}),
			policies:          getPolicies("testdata/v1beta1/simple-policy-td-aliases-in.yaml", t),
			wantAllow:         getProto("testdata/v1beta1/simple-policy-td-aliases-out.yaml", t),
		},
		{
			name:              "v1beta1 multiple trust domain aliases",
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td1", []string{"cluster.local", "some-td"}),
			policies:          getPolicies("testdata/v1beta1/simple-policy-multiple-td-aliases-in.yaml", t),
			wantAllow:         getProto("testdata/v1beta1/simple-policy-multiple-td-aliases-out.yaml", t),
		},
		{
			name:              "v1alpha1 one trust domain alias",
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td1", []string{"cluster.local"}),
			policies:          getPolicies("testdata/v1alpha1/simple-policy-td-aliases-in.yaml", t),
			wantAllow:         getProto("testdata/v1alpha1/simple-policy-td-aliases-out.yaml", t),
		},
		{
			name:              "v1alpha1 trust domain with * in principal",
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td1", []string{"foobar"}),
			policies:          getPolicies("testdata/v1alpha1/simple-policy-user-with-wildcard-in.yaml", t),
			wantAllow:         getProto("testdata/v1alpha1/simple-policy-user-with-wildcard-out.yaml", t),
		},
		{
			name:              "v1beta1 trust domain with * in principal",
			trustDomainBundle: trustdomain.NewTrustDomainBundle("td1", []string{"foobar"}),
			policies:          getPolicies("testdata/v1beta1/simple-policy-principal-with-wildcard-in.yaml", t),
			wantAllow:         getProto("testdata/v1beta1/simple-policy-principal-with-wildcard-out.yaml", t),
		},
		{
			name:              "v1alpha1 trust domain aliases with source.principal",
			trustDomainBundle: trustdomain.NewTrustDomainBundle("new-td", []string{"old-td", "some-trustdomain"}),
			policies:          getPolicies("testdata/v1alpha1/td-aliases-source-principal-in.yaml", t),
			wantAllow:         getProto("testdata/v1alpha1/td-aliases-source-principal-out.yaml", t),
		},
		{
			name:              "v1beta1 trust domain aliases with source.principal",
			trustDomainBundle: trustdomain.NewTrustDomainBundle("new-td", []string{"old-td", "some-trustdomain"}),
			policies:          getPolicies("testdata/v1beta1/td-aliases-source-principal-in.yaml", t),
			wantAllow:         getProto("testdata/v1beta1/td-aliases-source-principal-out.yaml", t),
		},
	}

	httpbinLabels := map[string]string{
		"app":     "httpbin",
		"version": "v1",
	}
	service := newService("httpbin.foo.svc.cluster.local", httpbinLabels, t)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder(tc.trustDomainBundle, service, labels.Collection{httpbinLabels}, "foo", tc.policies)
			if b == nil {
				t.Fatalf("failed to create builder")
			}
			gotDeny, gotAllow := b.generator.Generate(tc.forTCPFilter)
			verify(t, gotDeny, tc.wantDeny)
			verify(t, gotAllow, tc.wantAllow)
		})
	}
}

func getPolicies(filename string, t *testing.T) *model.AuthorizationPolicies {
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
	return policy.NewAuthzPolicies(configs, t)
}

func getProto(filename string, t *testing.T) *http_config.RBAC {
	t.Helper()
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		t.Fatalf("failed to read output yaml file: %v", err)
	}
	out := &http_config.RBAC{}
	if err := protomarshal.ApplyYAML(string(data), out); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return out
}

func verify(t *testing.T, got, want proto.Message) {
	if !reflect.DeepEqual(got, want) {
		gotYaml, err := protomarshal.ToYAML(got)
		if err != nil {
			t.Fatalf("failed to convert to YAML: %v", err)
		}
		wantYaml, err := protomarshal.ToYAML(want)
		if err != nil {
			t.Fatalf("failed to convert to YAML: %v", err)
		}
		t.Errorf("got:\n%s\nwant:\n%s\n", gotYaml, wantYaml)
	}
}
