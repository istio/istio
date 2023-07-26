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
	"regexp"
	"strings"
	"testing"

	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestRequestPrincipal(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in: "*",
			want: `
        and_ids:
          ids:
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: iss
              value:
                string_match:
                  safe_regex: {regex: .+}
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: sub
              value:
                string_match:
                  safe_regex: {regex: .+}
`,
		},
		{
			in: "foo*",
			want: `
        and_ids:
          ids:
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: iss
              value:
                string_match:
                  prefix: foo
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: sub
              value:
                string_match:
                  safe_regex: {regex: .+}
`,
		},
		{
			in: "foo/*",
			want: `
        and_ids:
          ids:
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: iss
              value:
                string_match:
                  exact: foo
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: sub
              value:
                string_match:
                  safe_regex: {regex: .+}
`,
		},
		{
			in: "foo/bar*",
			want: `
        and_ids:
          ids:
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: iss
              value:
                string_match:
                  exact: foo
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: sub
              value:
                string_match:
                  prefix: bar
`,
		},
		{
			in: "*foo",
			want: `
        and_ids:
          ids:
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: iss
              value:
                string_match:
                  safe_regex: {regex: .+}
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: sub
              value:
                string_match:
                  suffix: foo
`,
		},
		{
			in: "*/foo",
			want: `
        and_ids:
          ids:
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: iss
              value:
                string_match:
                  safe_regex: {regex: .+}
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: sub
              value:
                string_match:
                  exact: foo
`,
		},
		{
			in: "*bar/foo",
			want: `
        and_ids:
          ids:
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: iss
              value:
                string_match:
                  suffix: bar
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: sub
              value:
                string_match:
                  exact: foo
`,
		},
		{
			in: "foo/bar",
			want: `
        and_ids:
          ids:
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: iss
              value:
                string_match:
                  exact: foo
          - metadata:
              filter: envoy.filters.http.jwt_authn
              path:
              - key: payload
              - key: sub
              value:
                string_match:
                  exact: bar
`,
		},
	}
	rpg := requestPrincipalGenerator{}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := rpg.extendedPrincipal("", []string{tc.in}, false)
			if err != nil {
				t.Fatal(err)
			}
			principal := yamlPrincipal(t, tc.want)
			if diff := cmp.Diff(got, principal, protocmp.Transform()); diff != "" {
				t.Errorf("diff detected: %v", diff)
			}
		})
	}
}

func TestGenerator(t *testing.T) {
	cases := []struct {
		name   string
		g      generator
		key    string
		value  string
		forTCP bool
		want   any
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
         filter_state:
           key: io.istio.peer_principal
           string_match:
            safeRegex:
              regex: .*/ns/foo/.*`),
		},
		{
			name:   "srcNamespaceGenerator-tcp",
			g:      srcNamespaceGenerator{},
			value:  "foo",
			forTCP: true,
			want: yamlPrincipal(t, `
         filter_state:
           key: io.istio.peer_principal
           string_match:
            safeRegex:
              regex: .*/ns/foo/.*`),
		},
		{
			name:  "srcPrincipalGenerator-http",
			g:     srcPrincipalGenerator{},
			key:   "source.principal",
			value: "foo",
			want: yamlPrincipal(t, `
         filter_state:
           key: io.istio.peer_principal
           string_match:
            exact: spiffe://foo`),
		},
		{
			name:   "srcPrincipalGenerator-tcp",
			g:      srcPrincipalGenerator{},
			key:    "source.principal",
			value:  "foo",
			forTCP: true,
			want: yamlPrincipal(t, `
         filter_state:
           key: io.istio.peer_principal
           string_match:
            exact: spiffe://foo`),
		},
		{
			name:  "requestHeaderGenerator",
			g:     requestHeaderGenerator{},
			key:   "request.headers[x-foo]",
			value: "foo",
			want: yamlPrincipal(t, `
        header:
          name: x-foo
          stringMatch:
            exact: foo`),
		},
		{
			name:  "requestInlineHeaderGenerator",
			g:     requestInlineHeaderGenerator{},
			key:   "request.experimental.inline.headers[x-foo]",
			value: "foo",
			want: yamlPrincipal(t, `
         header:
          name: x-foo
          safeRegexMatch:
            regex: ^foo$|^foo,.*|.*,foo,.*|.*,foo$`),
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
			name:  "pathGenerator-template",
			g:     pathGenerator{},
			value: "/abc/{*}",
			want: yamlPermission(t, `
         uriTemplate:
           name: uri-template
           typedConfig:
            '@type': type.googleapis.com/envoy.extensions.path.match.uri_template.v3.UriTemplateMatchConfig
            pathTemplate: /abc/*`),
		},
		{
			name:  "methodGenerator",
			g:     methodGenerator{},
			value: "GET",
			want: yamlPermission(t, `
         header:
          name: :method
          stringMatch:
            exact: GET`),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got any
			var err error
			// nolint: gocritic
			if _, ok := tc.want.(*rbacpb.Permission); ok {
				got, err = tc.g.permission(tc.key, tc.value, tc.forTCP)
				if err != nil {
					t.Errorf("both permission and principal returned error")
				}
			} else if _, ok := tc.want.(*rbacpb.Principal); ok {
				got, err = tc.g.principal(tc.key, tc.value, tc.forTCP, false)
				if err != nil {
					t.Errorf("both permission and principal returned error")
				}
			} else {
				_, err1 := tc.g.principal(tc.key, tc.value, tc.forTCP, false)
				_, err2 := tc.g.permission(tc.key, tc.value, tc.forTCP)
				if err1 == nil || err2 == nil {
					t.Fatal("wanted error")
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

func TestServiceAccount(t *testing.T) {
	input := "my-ns/my-sa"
	cases := []struct {
		Name     string
		Identity string
		Match    bool
	}{
		{
			Name:     "standard",
			Identity: "spiffe://cluster.local/ns/my-ns/sa/my-sa",
			Match:    true,
		},
		{
			Name:     "suffix attributes",
			Identity: "spiffe://cluster.local/ns/my-ns/sa/my-sa/k/v",
			Match:    true,
		},
		{
			Name:     "prefix attributes",
			Identity: "spiffe://cluster.local/k/v/ns/my-ns/sa/my-sa",
			Match:    true,
		},
		{
			Name:     "middle attributes",
			Identity: "spiffe://cluster.local/ns/my-ns/k/v/sa/my-sa",
			Match:    true,
		},
		{
			Name:     "all attributes",
			Identity: "spiffe://cluster.local/k1/v1/ns/my-ns/k2/v2/sa/my-sa/k3/v3",
			Match:    true,
		},
		{
			Name:     "sa suffix string",
			Identity: "spiffe://cluster.local/ns/my-ns/sa/my-sa-suffix",
			Match:    false,
		},
		{
			Name:     "ns suffix string",
			Identity: "spiffe://cluster.local/ns/my-ns-suffix/sa/my-sa",
			Match:    false,
		},
		{
			Name:     "not spiffe",
			Identity: "cluster.local/ns/my-ns/sa/my-sa",
			Match:    false,
		},
		{
			Name:     "invalid spiffe",
			Identity: "spiffe://ns/my-ns/sa/my-sa",
			Match:    false,
		},
		{
			Name:     "missing sa",
			Identity: "spiffe://cluster.local/ns/my-ns",
			Match:    false,
		},
		{
			Name:     "missing ns",
			Identity: "spiffe://cluster.local/sa/my-sa",
			Match:    false,
		},
		{
			Name:     "missing ns",
			Identity: "spiffe://cluster.local/sa/my-sa",
			Match:    false,
		},
		{
			Name: "weird keys",
			// This test case matches when it shouldn't ideally.
			// Spiffe is a set of k/v pairs. Here we are accidentally matching
			// on previous value + next key when we shouldn't.
			// The chance of an identity being formatted like this is exceptionally low though
			Identity: "spiffe://cluster.local/" + strings.Join([]string{
				"k", "ns",
				"my-ns", "something",
				"bar", "sa",
				"my-sa", "baz",
			}, "/"),
			Match: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			r := serviceAccountRegex("", input)
			// Parse as regex. Envoy does a full string match, so handle that
			rgx, err := regexp.Compile("^" + r + "$")
			if err != nil {
				t.Fatal(err)
			}
			assert.Equal(t, rgx.MatchString(tt.Identity), tt.Match)
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

func TestServiceAccountRegex(t *testing.T) {
	assert.Equal(t, serviceAccountRegex("", "my-ns/my-sa"), `spiffe://.+/ns/my-ns/(.+/|)sa/my-sa(/.+)?`)
	assert.Equal(t, serviceAccountRegex("my-ns", "my-sa"), `spiffe://.+/ns/my-ns/(.+/|)sa/my-sa(/.+)?`)
}
