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
	"istio.io/istio/pilot/pkg/model"
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
					Hostname: model.Hostname("svc-name.test-ns"),
				},
				Labels:         model.Labels{"version": "v1"},
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
				t.Errorf("got error %q but want %q", err, tc.wantError)
			}
		} else {
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("got %v but want %v", *got, tc.want)
			}
			if got.GetNamespace() != tc.namespace {
				t.Errorf("got namespace %s but want %s", got.GetNamespace(), tc.namespace)
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

	got := NewModel(role, []*istio_rbac.ServiceRoleBinding{binding1, binding2})
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

func TestModel_Generate(t *testing.T) {
	serviceFoo := "foo.default.svc.cluster.local"
	serviceInstance := &model.ServiceInstance{
		Service: &model.Service{
			Hostname: model.Hostname(serviceFoo),
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
