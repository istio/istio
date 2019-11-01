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
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"

	istio_rbac "istio.io/api/rbac/v1alpha1"
	security "istio.io/api/security/v1beta1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
)

func TestNewServiceMetadata(t *testing.T) {
	testCases := []struct {
		name            string
		namespace       string
		serviceInstance *model.ServiceInstance
		want            ServiceMetadata
		wantError       string
	}{
		{
			name:            "empty-namespace",
			serviceInstance: &model.ServiceInstance{},
			wantError:       "found empty namespace",
		},
		{
			name:      "svc-name",
			namespace: "test-ns",
			serviceInstance: &model.ServiceInstance{
				Service: &model.Service{
					Hostname: host.Name("svc-name.test-ns"),
				},
				Labels:         labels.Instance{"version": "v1"},
				ServiceAccount: "spiffe://xyz.com/sa/service-account/ns/test-ns",
			},
			want: ServiceMetadata{
				Name:   "svc-name.test-ns",
				Labels: map[string]string{"version": "v1"},
				Attributes: map[string]string{
					attrDestName:      "svc-name",
					attrDestNamespace: "test-ns",
					attrDestUser:      "service-account",
				},
			},
		},
	}

	for _, tc := range testCases {
		got, err := NewServiceMetadata(tc.name, tc.namespace, tc.serviceInstance)

		if tc.wantError != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("got error %q but want %q", err, tc.wantError)
			}
		} else {
			if err != nil {
				t.Fatalf("unexpected error %q", err)
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Fatalf("got %v but want %v", *got, tc.want)
			}
			if got.GetNamespace() != tc.namespace {
				t.Fatalf("got namespace %s but want %s", got.GetNamespace(), tc.namespace)
			}
		}
	}
}

func TestNewModel(t *testing.T) {
	role := &istio_rbac.ServiceRole{
		Rules: []*istio_rbac.AccessRule{
			fullRule("perm-1"),
			fullRule("perm-2"),
		},
	}
	binding1 := &istio_rbac.ServiceRoleBinding{
		Subjects: []*istio_rbac.Subject{
			fullSubject("id-1"),
			fullSubject("id-2"),
		},
	}
	binding2 := &istio_rbac.ServiceRoleBinding{
		Subjects: []*istio_rbac.Subject{
			fullSubject("id-3"),
			fullSubject("id-4"),
		},
	}

	got := NewModelV1alpha1(trustdomain.NewTrustDomainBundle("", nil), role, []*istio_rbac.ServiceRoleBinding{binding1, binding2})
	want := Model{
		Permissions: []Permission{
			fullPermission("perm-1"),
			fullPermission("perm-2"),
		},
		Principals: []Principal{
			fullPrincipal("id-1"),
			fullPrincipal("id-2"),
			fullPrincipal("id-3"),
			fullPrincipal("id-4"),
		},
	}
	if !reflect.DeepEqual(*got, want) {
		t.Errorf("got %v but want %v", *got, want)
	}
}

func TestNewModelV1beta1(t *testing.T) {
	testCases := []struct {
		name string
		rule *security.Rule
		want Model
	}{
		{
			name: "only from",
			rule: &security.Rule{
				From: []*security.Rule_From{
					{
						Source: &security.Source{
							Principals: []string{"principal"},
						},
					},
				},
			},
			want: Model{
				Principals: []Principal{
					{
						Names:      []string{"principal"},
						Properties: []KeyValues{},
						v1beta1:    true,
					},
				},
				Permissions: []Permission{
					{
						AllowAll: true,
						v1beta1:  true,
					},
				},
			},
		},
		{
			name: "only to",
			rule: &security.Rule{
				To: []*security.Rule_To{
					{
						Operation: &security.Operation{
							Hosts: []string{"host"},
						},
					},
				},
			},
			want: Model{
				Principals: []Principal{
					{
						AllowAll: true,
						v1beta1:  true,
					},
				},
				Permissions: []Permission{
					{
						Hosts:       []string{"host"},
						Constraints: []KeyValues{},
						v1beta1:     true,
					},
				},
			},
		},
		{
			name: "one permission condition",
			rule: &security.Rule{
				When: []*security.Condition{
					newCondition(attrDestIP),
				},
			},
			want: Model{
				Permissions: []Permission{
					{
						Constraints: []KeyValues{
							{"destination.ip": []string{"value-destination.ip-1", "value-destination.ip-2"}},
						},
						v1beta1: true,
					},
				},
				Principals: []Principal{
					{
						AllowAll: true,
						v1beta1:  true,
					},
				},
			},
		},
		{
			name: "one principal condition",
			rule: &security.Rule{
				When: []*security.Condition{
					newCondition(attrRequestHeader),
				},
			},
			want: Model{
				Permissions: []Permission{
					{
						AllowAll: true,
						v1beta1:  true,
					},
				},
				Principals: []Principal{
					{
						Properties: []KeyValues{
							{"request.headers": []string{"value-request.headers-1", "value-request.headers-2"}},
						},
						v1beta1: true,
					},
				},
			},
		},
		{
			name: "full rule",
			rule: &security.Rule{
				From: []*security.Rule_From{
					{
						Source: &security.Source{
							Principals:        []string{"p1", "p2"},
							RequestPrincipals: []string{"rp1", "rp2"},
							IpBlocks:          []string{"1.1.1.1", "2.2.2.2"},
							Namespaces:        []string{"ns1", "ns2"},
						},
					},
					{
						Source: &security.Source{
							Principals: []string{"p3"},
						},
					},
				},
				To: []*security.Rule_To{
					{
						Operation: &security.Operation{
							Hosts:   []string{"h1", "h2"},
							Ports:   []string{"10", "20"},
							Paths:   []string{"/p1", "/p2"},
							Methods: []string{"m1", "m2"},
						},
					},
					{
						Operation: &security.Operation{
							Hosts: []string{"h3"},
						},
					},
				},
				When: []*security.Condition{
					newCondition(attrRequestHeader),
					newCondition(attrSrcIP),
					newCondition(attrSrcNamespace),
					newCondition(attrSrcUser),
					newCondition(attrSrcPrincipal),
					newCondition(attrRequestPrincipal),
					newCondition(attrRequestAudiences),
					newCondition(attrRequestPresenter),
					newCondition(attrRequestClaims),
					newCondition(attrRequestClaimGroups),
					newCondition(attrDestIP),
					newCondition(attrDestPort),
					newCondition(attrDestLabel),     // Should be ignored.
					newCondition(attrDestName),      // Should be ignored.
					newCondition(attrDestNamespace), // Should be ignored.
					newCondition(attrDestUser),      // Should be ignored.
					newCondition(attrConnSNI),
				},
			},
			want: Model{
				Permissions: []Permission{
					{
						Hosts:   []string{"h1", "h2"},
						Paths:   []string{"/p1", "/p2"},
						Methods: []string{"m1", "m2"},
						Ports:   []string{"10", "20"},
						Constraints: []KeyValues{
							{"destination.ip": []string{"value-destination.ip-1", "value-destination.ip-2"}},
							{"destination.port": []string{"value-destination.port-1", "value-destination.port-2"}},
							{"connection.sni": []string{"value-connection.sni-1", "value-connection.sni-2"}},
						},
						v1beta1: true,
					},
					{
						Hosts: []string{"h3"},
						Constraints: []KeyValues{
							{"destination.ip": []string{"value-destination.ip-1", "value-destination.ip-2"}},
							{"destination.port": []string{"value-destination.port-1", "value-destination.port-2"}},
							{"connection.sni": []string{"value-connection.sni-1", "value-connection.sni-2"}},
						},
						v1beta1: true,
					},
				},
				Principals: []Principal{
					{
						Names:             []string{"p1", "p2"},
						Namespaces:        []string{"ns1", "ns2"},
						IPs:               []string{"1.1.1.1", "2.2.2.2"},
						RequestPrincipals: []string{"rp1", "rp2"},
						Properties: []KeyValues{
							{"request.headers": []string{"value-request.headers-1", "value-request.headers-2"}},
							{"source.ip": []string{"value-source.ip-1", "value-source.ip-2"}},
							{"source.namespace": []string{"value-source.namespace-1", "value-source.namespace-2"}},
							{"source.user": []string{"value-source.user-1", "value-source.user-2"}},
							{"source.principal": []string{"value-source.principal-1", "value-source.principal-2"}},
							{"request.auth.principal": []string{"value-request.auth.principal-1", "value-request.auth.principal-2"}},
							{"request.auth.audiences": []string{"value-request.auth.audiences-1", "value-request.auth.audiences-2"}},
							{"request.auth.presenter": []string{"value-request.auth.presenter-1", "value-request.auth.presenter-2"}},
							{"request.auth.claims": []string{"value-request.auth.claims-1", "value-request.auth.claims-2"}},
							{"request.auth.claims[groups]": []string{"value-request.auth.claims[groups]-1", "value-request.auth.claims[groups]-2"}},
						},
						v1beta1: true,
					},
					{
						Names: []string{"p3"},
						Properties: []KeyValues{
							{"request.headers": []string{"value-request.headers-1", "value-request.headers-2"}},
							{"source.ip": []string{"value-source.ip-1", "value-source.ip-2"}},
							{"source.namespace": []string{"value-source.namespace-1", "value-source.namespace-2"}},
							{"source.user": []string{"value-source.user-1", "value-source.user-2"}},
							{"source.principal": []string{"value-source.principal-1", "value-source.principal-2"}},
							{"request.auth.principal": []string{"value-request.auth.principal-1", "value-request.auth.principal-2"}},
							{"request.auth.audiences": []string{"value-request.auth.audiences-1", "value-request.auth.audiences-2"}},
							{"request.auth.presenter": []string{"value-request.auth.presenter-1", "value-request.auth.presenter-2"}},
							{"request.auth.claims": []string{"value-request.auth.claims-1", "value-request.auth.claims-2"}},
							{"request.auth.claims[groups]": []string{"value-request.auth.claims[groups]-1", "value-request.auth.claims[groups]-2"}},
						},
						v1beta1: true,
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewModelV1beta1(trustdomain.NewTrustDomainBundle("", nil), tc.rule)
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("\n got %+v\nwant %+v", *got, tc.want)
			}
		})
	}
}

func TestModel_Generate(t *testing.T) {
	serviceFoo := "foo.default.svc.cluster.local"
	serviceInstance := &model.ServiceInstance{
		Service: &model.Service{
			Hostname: host.Name(serviceFoo),
		},
	}
	serviceMetadata, _ := NewServiceMetadata("foo", "default", serviceInstance)
	testCases := []struct {
		name           string
		permissions    []Permission
		principals     []Principal
		forTCPFilter   bool
		wantPermission []string
		wantPrincipal  []string
	}{
		{
			name: "permission list empty",
			principals: []Principal{
				simplePrincipal("id-1"),
				simplePrincipal("id-2"),
			},
		},
		{
			name: "permission not matched",
			permissions: []Permission{
				simplePermission("bar-not-matched", "perm-1"),
				simplePermission("baz-not-matched", "perm-2"),
			},
			principals: []Principal{
				simplePrincipal("id-1"),
				simplePrincipal("id-2"),
			},
		},
		{
			name: "principal list empty",
			permissions: []Permission{
				simplePermission(serviceFoo, "perm-1"),
				simplePermission(serviceFoo, "perm-2"),
			},
		},
		{
			name: "permission and principal",
			permissions: []Permission{
				simplePermission(serviceFoo, "perm-1"),
				simplePermission("bar-not-matched", "perm-2"),
				simplePermission(serviceFoo, "perm-3"),
				simplePermission("baz-not-matched", "perm-4"),
			},
			principals: []Principal{
				simplePrincipal("id-1"),
				simplePrincipal("id-2"),
			},
			wantPermission: []string{
				permissionTag("perm-1"),
				permissionTag("perm-3"),
			},
			wantPrincipal: []string{
				principalTag("id-1"),
				principalTag("id-2"),
			},
		},
		{
			name: "forTCPFilter: permission and principal",
			permissions: []Permission{
				simplePermission(serviceFoo, "perm-1"),
				simplePermission(serviceFoo, "perm-2"),
			},
			principals: []Principal{
				simplePrincipal("id-1"),
				simplePrincipal("id-2"),
			},
			forTCPFilter: true,
		},
	}

	for _, tc := range testCases {
		m := Model{
			Permissions: tc.permissions,
			Principals:  tc.principals,
		}
		got := m.Generate(serviceMetadata, tc.forTCPFilter)
		if len(tc.wantPermission) == 0 || len(tc.wantPrincipal) == 0 {
			if got != nil {
				t.Errorf("%s: got %v but want nil", tc.name, *got)
			}
		} else {
			if len(got.GetPermissions()) != len(tc.wantPermission) {
				t.Errorf("%s: got %d permissions but want %d",
					tc.name, len(got.GetPermissions()), len(tc.wantPermission))
			} else {
				for i, wantPermission := range tc.wantPermission {
					gotStr := spew.Sdump(got.Permissions[i])
					if !strings.Contains(gotStr, wantPermission) {
						t.Errorf("%s: not found %q in permission %s", tc.name, wantPermission, gotStr)
					}
				}
			}
			if len(got.GetPrincipals()) != len(tc.wantPrincipal) {
				t.Errorf("%s: got %d principals but want %d",
					tc.name, len(got.GetPrincipals()), len(tc.wantPrincipal))
			} else {
				for i, wantPrincipal := range tc.wantPrincipal {
					gotStr := spew.Sdump(got.Principals[i])
					if !strings.Contains(gotStr, wantPrincipal) {
						t.Errorf("%s: not found %q in principal %s", tc.name, wantPrincipal, gotStr)
					}
				}
			}
		}
	}
}
