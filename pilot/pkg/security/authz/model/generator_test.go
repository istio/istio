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

package model

import (
	"testing"

	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pkg/util/protomarshal"
)

func TestGenerator(t *testing.T) {
	cases := []struct {
		name   string
		g      generator
		key    string
		value  string
		forTCP bool
		want   interface{}
	}{
		{
			name:  "destIPGenerator",
			g:     destIPGenerator{},
			value: "1.2.3.4",
			want: yamlPermission(t, `
         destinationIp:
          addressPrefix: 1.2.3.4
          prefixLen: 32`),
		},
		{
			name:  "destPortGenerator",
			g:     destPortGenerator{},
			value: "80",
			want: yamlPermission(t, `
         destinationPort: 80`),
		},
		{
			name:  "connSNIGenerator",
			g:     connSNIGenerator{},
			value: "exact.com",
			want: yamlPermission(t, `
         requestedServerName:
          exact: exact.com`),
		},
		{
			name:  "envoyFilterGenerator-string",
			g:     envoyFilterGenerator{},
			key:   "experimental.a.b.c[d]",
			value: "val",
			want: yamlPermission(t, `
         metadata:
          filter: a.b.c
          path:
          - key: d
          value:
            stringMatch:
              exact: val`),
		},
		{
			name:  "envoyFilterGenerator-invalid",
			g:     envoyFilterGenerator{},
			key:   "experimental.a.b.c]",
			value: "val",
		},
		{
			name:  "envoyFilterGenerator-list",
			g:     envoyFilterGenerator{},
			key:   "experimental.a.b.c[d]",
			value: "[v1, v2]",
			want: yamlPermission(t, `
         metadata:
          filter: a.b.c
          path:
          - key: d
          value:
            listMatch:
              oneOf:
                stringMatch:
                  exact: v1, v2`),
		},
		{
			name:  "srcIPGenerator",
			g:     srcIPGenerator{},
			value: "1.2.3.4",
			want: yamlPrincipal(t, `
         directRemoteIp:
          addressPrefix: 1.2.3.4
          prefixLen: 32`),
		},
		{
			name:  "remoteIPGenerator",
			g:     remoteIPGenerator{},
			value: "1.2.3.4",
			want: yamlPrincipal(t, `
         remoteIp:
          addressPrefix: 1.2.3.4
          prefixLen: 32`),
		},
		{
			name:  "srcNamespaceGenerator-http",
			g:     srcNamespaceGenerator{},
			value: "foo",
			want: yamlPrincipal(t, `
         authenticated:
          principalName:
            safeRegex:
              googleRe2: {}
              regex: .*/ns/foo/.*`),
		},
		{
			name:   "srcNamespaceGenerator-tcp",
			g:      srcNamespaceGenerator{},
			value:  "foo",
			forTCP: true,
			want: yamlPrincipal(t, `
         authenticated:
          principalName:
            safeRegex:
              googleRe2: {}
              regex: .*/ns/foo/.*`),
		},
		{
			name:  "srcPrincipalGenerator-http",
			g:     srcPrincipalGenerator{},
			key:   "source.principal",
			value: "foo",
			want: yamlPrincipal(t, `
         authenticated:
          principalName:
            exact: spiffe://foo`),
		},
		{
			name:   "srcPrincipalGenerator-tcp",
			g:      srcPrincipalGenerator{},
			key:    "source.principal",
			value:  "foo",
			forTCP: true,
			want: yamlPrincipal(t, `
         authenticated:
          principalName:
            exact: spiffe://foo`),
		},
		{
			name:  "requestPrincipalGenerator",
			g:     requestPrincipalGenerator{},
			key:   "request.auth.principal",
			value: "foo",
			want: yamlPrincipal(t, `
         metadata:
          filter: istio_authn
          path:
          - key: request.auth.principal
          value:
            stringMatch:
              exact: foo`),
		},
		{
			name:  "requestAudiencesGenerator",
			g:     requestAudiencesGenerator{},
			key:   "request.auth.audiences",
			value: "foo",
			want: yamlPrincipal(t, `
         metadata:
          filter: istio_authn
          path:
          - key: request.auth.audiences
          value:
            stringMatch:
              exact: foo`),
		},
		{
			name:  "requestPresenterGenerator",
			g:     requestPresenterGenerator{},
			key:   "request.auth.presenter",
			value: "foo",
			want: yamlPrincipal(t, `
         metadata:
          filter: istio_authn
          path:
          - key: request.auth.presenter
          value:
            stringMatch:
              exact: foo`),
		},
		{
			name:  "requestHeaderGenerator",
			g:     requestHeaderGenerator{},
			key:   "request.headers[x-foo]",
			value: "foo",
			want: yamlPrincipal(t, `
         header:
          exactMatch: foo
          name: x-foo`),
		},
		{
			name:  "requestClaimGenerator",
			g:     requestClaimGenerator{},
			key:   "request.auth.claims[bar]",
			value: "foo",
			want: yamlPrincipal(t, `
         metadata:
          filter: istio_authn
          path:
          - key: request.auth.claims
          - key: bar
          value:
            listMatch:
              oneOf:
                stringMatch:
                  exact: foo`),
		},
		{
			name:  "requestNestedClaimsGenerator",
			g:     requestClaimGenerator{},
			key:   "request.auth.claims[bar][baz]",
			value: "foo",
			want: yamlPrincipal(t, `
         metadata:
          filter: istio_authn
          path:
          - key: request.auth.claims
          - key: bar
          - key: baz
          value:
            listMatch:
              oneOf:
                stringMatch:
                  exact: foo`),
		},
		{
			name:  "hostGenerator",
			g:     hostGenerator{},
			value: "foo",
			want: yamlPermission(t, `
         header:
          stringMatch:
            exact: foo
            ignoreCase: true
          name: :authority`),
		},
		{
			name:  "pathGenerator",
			g:     pathGenerator{},
			value: "/abc",
			want: yamlPermission(t, `
         urlPath:
          path:
            exact: /abc`),
		},
		{
			name:  "methodGenerator",
			g:     methodGenerator{},
			value: "GET",
			want: yamlPermission(t, `
         header:
          exactMatch: GET
          name: :method`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got interface{}
			var err error
			// nolint: gocritic
			if _, ok := tc.want.(*rbacpb.Permission); ok {
				got, err = tc.g.permission(tc.key, tc.value, tc.forTCP)
				if err != nil {
					t.Errorf("both permission and principal returned error")
				}
			} else if _, ok := tc.want.(*rbacpb.Principal); ok {
				got, err = tc.g.principal(tc.key, tc.value, tc.forTCP)
				if err != nil {
					t.Errorf("both permission and principal returned error")
				}
			} else {
				_, err1 := tc.g.principal(tc.key, tc.value, tc.forTCP)
				_, err2 := tc.g.permission(tc.key, tc.value, tc.forTCP)
				if err1 == nil || err2 == nil {
					t.Fatalf("wanted error")
				}
				return
			}
			if diff := cmp.Diff(got, tc.want, protocmp.Transform()); diff != "" {
				var gotYaml string
				gotProto, ok := got.(proto.Message)
				if !ok {
					t.Fatal("failed to extract proto")
				}
				if gotYaml, err = protomarshal.ToYAML(gotProto); err != nil {
					t.Fatalf("%s: failed to parse yaml: %s", tc.name, err)
				}
				t.Errorf("got:\n %v\n but want:\n %v", gotYaml, tc.want)
			}
		})
	}
}

func yamlPermission(t *testing.T, yaml string) *rbacpb.Permission {
	t.Helper()
	p := &rbacpb.Permission{}
	if err := protomarshal.ApplyYAML(yaml, p); err != nil {
		t.Fatalf("failed to parse yaml: %s", err)
	}
	return p
}

func yamlPrincipal(t *testing.T, yaml string) *rbacpb.Principal {
	t.Helper()
	p := &rbacpb.Principal{}
	if err := protomarshal.ApplyYAML(yaml, p); err != nil {
		t.Fatalf("failed to parse yaml: %s", err)
	}
	return p
}
