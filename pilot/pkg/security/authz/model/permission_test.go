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

package model

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"

	"istio.io/istio/pkg/util/protomarshal"
)

func TestPermission_Match(t *testing.T) {
	cases := []struct {
		name    string
		service *ServiceMetadata
		perm    *Permission
		want    bool
	}{
		{
			name:    "empty permission",
			service: &ServiceMetadata{},
			want:    true,
		},
		{
			name: "service.name not matched",
			service: &ServiceMetadata{
				Name:       "product.default",
				Attributes: map[string]string{"destination.name": "s2"},
			},
			perm: &Permission{
				Services: []string{"review.default"},
				Constraints: []KeyValues{
					{
						"destination.name": Values{
							Values: []string{"s1", "s2"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "destination.name not matched",
			service: &ServiceMetadata{
				Name:       "product.default",
				Attributes: map[string]string{"destination.name": "s3"},
			},
			perm: &Permission{
				Services: []string{"product.default"},
				Constraints: []KeyValues{
					{
						"destination.name": Values{
							Values: []string{"s1", "s2"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "destination.labels not matched",
			service: &ServiceMetadata{
				Name:   "product.default",
				Labels: map[string]string{"token": "t3"},
			},
			perm: &Permission{
				Services: []string{
					"product.default",
				},
				Constraints: []KeyValues{
					{
						"destination.labels[token]": Values{
							Values: []string{"t1", "t2"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "destination.user not matched",
			service: &ServiceMetadata{
				Name:       "product.default",
				Labels:     map[string]string{"token": "t3"},
				Attributes: map[string]string{"destination.user": "user1"},
			},
			perm: &Permission{
				Services: []string{"product.default"},
				Constraints: []KeyValues{
					{
						"destination.user": Values{
							Values: []string{"user2"},
						},
					},
				},
			},
			want: false,
		},
		{
			name: "all matched",
			service: &ServiceMetadata{
				Name: "product.default",
				Attributes: map[string]string{
					"destination.name":      "s2",
					"destination.namespace": "ns2",
					"destination.user":      "sa2",
					"other":                 "other",
				},
				Labels: map[string]string{"token": "t2"},
			},
			perm: &Permission{
				Services: []string{"product.default"},
				Constraints: []KeyValues{
					{
						"destination.name": Values{
							Values: []string{"s1", "s2"},
						},
					},
					{
						"destination.namespace": Values{
							Values: []string{"ns1", "ns2"},
						},
					},
					{
						"destination.user": Values{
							Values: []string{"sa1", "sa2"},
						},
					},
					{
						"destination.labels[token]": Values{
							Values: []string{"t1", "t2"},
						},
					},
					{
						"request.headers[user-agent]": Values{
							Values: []string{"x1", "x2"},
						},
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range cases {
		if actual := tc.perm.Match(tc.service); actual != tc.want {
			t.Errorf("%s: want %v but got %v", tc.name, tc.want, actual)
		}
	}
}

func TestPermission_ValidateForTCP(t *testing.T) {
	testCases := []struct {
		name string
		perm *Permission
		want bool
	}{
		{
			name: "nil permission",
			want: true,
		},
		{
			name: "permission with path",
			perm: &Permission{
				Paths: []string{"/"},
			},
		},
		{
			name: "permission with notPath",
			perm: &Permission{
				NotPaths: []string{"/"},
			},
		},
		{
			name: "permission with method",
			perm: &Permission{
				Methods: []string{"GET"},
			},
		},
		{
			name: "permission with notMethod",
			perm: &Permission{
				NotMethods: []string{"GET"},
			},
		},
		{
			name: "permission with unsupported constraint",
			perm: &Permission{
				Constraints: []KeyValues{
					{
						attrDestIP: Values{
							Values: []string{"1.2.3.4"},
						},
					},
					{
						attrRequestHeader: Values{
							Values: []string{"TOKEN"},
						},
					},
				},
			},
		},
		{
			name: "good permission",
			perm: &Permission{
				Constraints: []KeyValues{
					{
						attrDestIP: Values{
							Values: []string{"1.2.3.4"},
						},
					},
					{
						attrDestPort: Values{
							Values: []string{"80"},
						},
					},
				},
			},
			want: true,
		},
	}
	for _, tc := range testCases {
		err := tc.perm.ValidateForTCP(true)
		got := err == nil
		if tc.want != got {
			t.Errorf("%s: want %v bot got: %s", tc.name, tc.want, err)
		}
	}
}

func TestPermission_Generate(t *testing.T) {
	testCases := []struct {
		name         string
		permission   *Permission
		forTCPFilter bool
		forDeny      bool
		wantYAML     string
		wantError    string
	}{
		{
			name: "nil permission",
		},
		{
			name:       "empty permission",
			permission: &Permission{},
			wantYAML: `
        andRules:
          rules:
          - any: true`,
		},
		{
			name: "allowAll permission",
			permission: &Permission{
				Hosts:    []string{"ignored"},
				AllowAll: true,
			},
			wantYAML: `
        andRules:
          rules:
          - any: true`,
		},
		{
			name: "permission with hosts",
			permission: &Permission{
				Hosts: []string{"host-1", "host-2"},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - header:
                  exactMatch: host-1
                  name: :authority
              - header:
                  exactMatch: host-2
                  name: :authority`,
		},
		{
			name: "permission with notHosts",
			permission: &Permission{
				NotHosts: []string{"host-1", "host-2"},
			},
			wantYAML: `
        andRules:
          rules:
          - notRule:
              orRules:
                rules:
                - header:
                    exactMatch: host-1
                    name: :authority
                - header:
                    exactMatch: host-2
                    name: :authority`,
		},
		{
			name: "permission with methods",
			permission: &Permission{
				Methods: []string{"GET", "POST"},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - header:
                  exactMatch: GET
                  name: :method
              - header:
                  exactMatch: POST
                  name: :method`,
		},
		{
			name: "permission with notMethods",
			permission: &Permission{
				NotMethods: []string{"GET", "POST"},
			},
			wantYAML: `
        andRules:
          rules:
          - notRule:
              orRules:
                rules:
                - header:
                    exactMatch: GET
                    name: :method
                - header:
                    exactMatch: POST
                    name: :method`,
		},
		{
			name: "permission with paths",
			permission: &Permission{
				Paths: []string{"/hello", "/world"},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - urlPath:
                  path:
                    exact: /hello
              - urlPath:
                  path:
                    exact: /world`,
		},
		{
			name: "permission with notPaths",
			permission: &Permission{
				NotPaths: []string{"/hello", "/world"},
			},
			wantYAML: `
        andRules:
          rules:
          - notRule:
              orRules:
                rules:
                - urlPath:
                    path:
                      exact: /hello
                - urlPath:
                    path:
                      exact: /world`,
		},
		{
			name: "permission with ports",
			permission: &Permission{
				Ports: []string{"80", "90"},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - destinationPort: 80
              - destinationPort: 90`,
		},
		{
			name: "permission with notPorts",
			permission: &Permission{
				NotPorts: []string{"80", "90"},
			},
			wantYAML: `
        andRules:
          rules:
          - notRule:
              orRules:
                rules:
                - destinationPort: 80
                - destinationPort: 90`,
		},
		{
			name: "permission with constraint attrDestIP",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						attrDestIP: Values{
							Values: []string{"1.2.3.4", "5.6.7.8"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - destinationIp:
                  addressPrefix: 1.2.3.4
                  prefixLen: 32
              - destinationIp:
                  addressPrefix: 5.6.7.8
                  prefixLen: 32`,
		},
		{
			name: "permission with constraint attrDestPort",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						attrDestPort: Values{
							Values: []string{"8000", "9000"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - destinationPort: 8000
              - destinationPort: 9000`,
		},
		{
			name: "permission with constraint pathHeader",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						pathHeader: Values{
							Values: []string{"/hello", "/world"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - header:
                  exactMatch: /hello
                  name: :path
              - header:
                  exactMatch: /world
                  name: :path`,
		},
		{
			name: "permission with constraint methodHeader",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						methodHeader: Values{
							Values: []string{"GET", "POST"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - header:
                  exactMatch: GET
                  name: :method
              - header:
                  exactMatch: POST
                  name: :method`,
		},
		{
			name: "permission with constraint hostHeader",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						hostHeader: Values{
							Values: []string{"istio.io", "github.com"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - header:
                  exactMatch: istio.io
                  name: :authority
              - header:
                  exactMatch: github.com
                  name: :authority`,
		},
		{
			name: "permission with constraint attrRequestHeader",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						fmt.Sprintf("%s[%s]", attrRequestHeader, "X-id"): Values{
							Values: []string{"id-1", "id-2"},
						},
						fmt.Sprintf("%s[%s]", attrRequestHeader, "X-tag"): Values{
							Values: []string{"tag-1", "tag-2"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - header:
                  exactMatch: id-1
                  name: X-id
              - header:
                  exactMatch: id-2
                  name: X-id
          - orRules:
              rules:
              - header:
                  exactMatch: tag-1
                  name: X-tag
              - header:
                  exactMatch: tag-2
                  name: X-tag`,
		},
		{
			name: "permission with constraint attrConnSNI",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						attrConnSNI: Values{
							Values: []string{"sni-1", "sni-2"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - requestedServerName:
                  exact: sni-1
              - requestedServerName:
                  exact: sni-2`,
		},
		{
			name: "permission with constraint experimental.envoy.filters.",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						"experimental.envoy.filters.a.b[c]": Values{
							Values: []string{"v1", "v2"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - metadata:
                  filter: envoy.filters.a.b
                  path:
                  - key: c
                  value:
                    stringMatch:
                      exact: v1
              - metadata:
                  filter: envoy.filters.a.b
                  path:
                  - key: c
                  value:
                    stringMatch:
                      exact: v2`,
		},
		{
			name: "permission with notValues",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						attrDestIP: Values{
							NotValues: []string{"1.2.3.4", "5.6.7.8"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - notRule:
              orRules:
                rules:
                - destinationIp:
                    addressPrefix: 1.2.3.4
                    prefixLen: 32
                - destinationIp:
                    addressPrefix: 5.6.7.8
                    prefixLen: 32`,
		},
		{
			name: "permission with values and notValues",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						attrDestIP: Values{
							Values:    []string{"1.2.3.4", "5.6.7.8"},
							NotValues: []string{"1.2.3.4", "5.6.7.8"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - destinationIp:
                  addressPrefix: 1.2.3.4
                  prefixLen: 32
              - destinationIp:
                  addressPrefix: 5.6.7.8
                  prefixLen: 32
          - notRule:
              orRules:
                rules:
                - destinationIp:
                    addressPrefix: 1.2.3.4
                    prefixLen: 32
                - destinationIp:
                    addressPrefix: 5.6.7.8
                    prefixLen: 32`,
		},
		{
			name: "permission with unknown constraint",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						"unknown": Values{
							Values: []string{"v1", "v2"},
						},
						attrDestIP: Values{
							Values: []string{"1.2.3.4"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - destinationIp:
                  addressPrefix: 1.2.3.4
                  prefixLen: 32`,
		},
		{
			name: "permission with invalid constraint",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						attrDestPort: Values{
							Values: []string{"9999999"},
						},
						attrDestIP: Values{
							Values: []string{"1.2.3.4"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - destinationIp:
                  addressPrefix: 1.2.3.4
                  prefixLen: 32`,
		},
		{
			name: "permission with multiple constraints",
			permission: &Permission{
				Constraints: []KeyValues{
					{
						attrDestIP: Values{
							Values: []string{"1.2.3.4", "5.6.7.8"},
						},
						attrDestPort: Values{
							Values: []string{"8000", "9000"},
						},
					},
					{
						pathHeader: Values{
							Values: []string{"/hello", "/world"},
						},
						methodHeader: Values{
							Values: []string{"GET", "POST"},
						},
					},
				},
			},
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - destinationIp:
                  addressPrefix: 1.2.3.4
                  prefixLen: 32
              - destinationIp:
                  addressPrefix: 5.6.7.8
                  prefixLen: 32
          - orRules:
              rules:
              - destinationPort: 8000
              - destinationPort: 9000
          - orRules:
              rules:
              - header:
                  exactMatch: GET
                  name: :method
              - header:
                  exactMatch: POST
                  name: :method
          - orRules:
              rules:
              - header:
                  exactMatch: /hello
                  name: :path
              - header:
                  exactMatch: /world
                  name: :path`,
		},
		{
			name: "permission with forDeny",
			permission: &Permission{
				Methods: []string{"GET"},
				Constraints: []KeyValues{
					{
						"request.headers[:foo]": Values{
							Values: []string{"bar"},
						},
					},
				},
			},
			forDeny: true,
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - header:
                  exactMatch: GET
                  name: :method
          - orRules:
              rules:
              - header:
                  exactMatch: bar
                  name: :foo`,
		},
		{
			name: "permission with forTCP",
			permission: &Permission{
				Methods: []string{"GET"},
				Constraints: []KeyValues{
					{
						"request.headers[:foo]": Values{
							Values: []string{"bar"},
						},
					},
				},
			},
			forTCPFilter: true,
			wantError:    "methods([GET])",
		},
		{
			name: "permission with forTCP and forDeny",
			permission: &Permission{
				Methods:    []string{"GET"},
				NotMethods: []string{"POST"},
				Paths:      []string{"/abc"},
				NotPaths:   []string{"/xyz"},
				Ports:      []string{"80"},
				NotPorts:   []string{"81"},
				Hosts:      []string{"host.com"},
				NotHosts:   []string{"other.com"},
				Constraints: []KeyValues{
					{
						attrDestIP: Values{
							Values: []string{"1.2.3.4"},
						},
						attrDestPort: Values{
							Values:    []string{"90"},
							NotValues: []string{"91"},
						},
					},
				},
			},
			forTCPFilter: true,
			forDeny:      true,
			wantYAML: `
        andRules:
          rules:
          - orRules:
              rules:
              - destinationPort: 80
          - notRule:
              orRules:
                rules:
                - destinationPort: 81
          - orRules:
              rules:
              - destinationIp:
                  addressPrefix: 1.2.3.4
                  prefixLen: 32
          - orRules:
              rules:
              - destinationPort: 90
          - notRule:
              orRules:
                rules:
                - destinationPort: 91`,
		},
		{
			name: "permission invalid for TCP",
			permission: &Permission{
				Hosts: []string{"host-1"},
			},
			forTCPFilter: true,
			wantError:    "hosts([host-1])",
		},
	}

	for _, tc := range testCases {
		got, err := tc.permission.Generate(tc.forTCPFilter, tc.forDeny)
		if tc.wantError != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Errorf("%s: got error %q but want error %q", tc.name, err, tc.wantError)
			}
		} else if err != nil {
			t.Errorf("%s: failed to generate permission: %s", tc.name, err)
		} else {
			var gotYaml string
			if got != nil {
				if gotYaml, err = protomarshal.ToYAML(got); err != nil {
					t.Fatalf("%s: failed to parse yaml: %s", tc.name, err)
				}
			}
			if tc.wantYAML == "" {
				if got != nil {
					t.Errorf("%s: got\n%s but want nil", tc.name, gotYaml)
				}
			} else {
				want := &envoy_rbac.Permission{}
				if err := protomarshal.ApplyYAML(tc.wantYAML, want); err != nil {
					t.Fatalf("%s: failed to parse yaml: %s", tc.name, err)
				}

				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s:\ngot:\n%s\nwant:\n%s", tc.name, gotYaml, tc.wantYAML)
				}
			}
		}
	}
}
