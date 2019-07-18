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

package policy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	"github.com/hashicorp/go-multierror"

	istio_rbac "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
)

func NewServiceMetadata(hostname string, labels map[string]string, t *testing.T) *authz_model.ServiceMetadata {
	t.Helper()
	splits := strings.Split(hostname, ".")
	if len(splits) < 2 {
		t.Fatalf("failed to initialize service instance: invalid hostname")
	}
	name := splits[0]
	namespace := splits[1]

	serviceInstance := &model.ServiceInstance{
		Service: &model.Service{
			Attributes: model.ServiceAttributes{
				Name:      name,
				Namespace: namespace,
			},
			Hostname: model.Hostname(hostname),
		},
		Labels: labels,
	}

	serviceMetadata, err := authz_model.NewServiceMetadata(name, namespace, serviceInstance)
	if err != nil {
		t.Fatalf("failed to initialize service instance: %s", err)
	}

	return serviceMetadata
}

func NewAuthzPolicies(policies []*model.Config, t *testing.T) *model.AuthorizationPolicies {
	t.Helper()

	hasClusterRbacConfig := false
	for _, p := range policies {
		if p.Type == model.ClusterRbacConfig.Type {
			hasClusterRbacConfig = true
			break
		}
	}
	if !hasClusterRbacConfig {
		policies = append(policies, simpleClusterRbacConfig())
	}

	store := model.MakeIstioStore(memory.Make(model.IstioConfigTypes))
	for _, p := range policies {
		if _, err := store.Create(*p); err != nil {
			t.Fatalf("failed to initilize authz policies: %s", err)
		}
	}

	authzPolicies, err := model.NewAuthzPolicies(&model.Environment{
		IstioConfigStore: store,
	})
	if err != nil {
		t.Fatalf("failed to create authz policies: %s", err)
	}

	return authzPolicies
}

func simpleClusterRbacConfig() *model.Config {
	config := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.ClusterRbacConfig.Type,
			Name:      "default",
			Namespace: "default",
		},
		Spec: &istio_rbac.RbacConfig{
			Mode: istio_rbac.RbacConfig_ON,
		},
	}
	return config
}

func RoleTag(name string) string {
	return fmt.Sprintf("MethodFromRole[%s]", name)
}

func SimpleRole(name string, namespace string, service string) *model.Config {
	spec := &istio_rbac.ServiceRole{
		Rules: []*istio_rbac.AccessRule{
			{
				Methods: []string{RoleTag(name)},
			},
		},
	}
	if service != "" {
		spec.Rules[0].Services = []string{fmt.Sprintf("%s.%s.svc.cluster.local", service, namespace)}
	}
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.ServiceRole.Type,
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

func BindingTag(name string) string {
	return fmt.Sprintf("UserFromBinding[%s]", name)
}

func SimpleBinding(name string, namespace string, role string) *model.Config {
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.ServiceRoleBinding.Type,
			Name:      name,
			Namespace: namespace,
		},
		Spec: &istio_rbac.ServiceRoleBinding{
			Subjects: []*istio_rbac.Subject{
				{
					User: BindingTag(name),
				},
			},
			RoleRef: &istio_rbac.RoleRef{
				Name: role,
				Kind: "ServiceRole",
			},
		},
	}
}

func SimplePermissiveBinding(name string, namespace string, role string) *model.Config {
	config := SimpleBinding(name, namespace, role)
	binding := config.Spec.(*istio_rbac.ServiceRoleBinding)
	binding.Mode = istio_rbac.EnforcementMode_PERMISSIVE
	return config
}

func AuthzPolicyTag(name string) string {
	return fmt.Sprintf("UserFromPolicy[%s]", name)
}

func SimpleAuthorizationPolicy(name string, namespace string, labels map[string]string, role string) *model.Config {
	config := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.AuthorizationPolicy.Type,
			Name:      name,
			Namespace: namespace,
		},
		Spec: &istio_rbac.AuthorizationPolicy{
			WorkloadSelector: &istio_rbac.WorkloadSelector{
				Labels: labels,
			},
			Allow: []*istio_rbac.ServiceRoleBinding{
				{
					Subjects: []*istio_rbac.Subject{
						{
							User: AuthzPolicyTag(name),
						},
					},
					Role: role,
				},
			},
		},
	}
	return config
}

func Verify(got *envoy_rbac.RBAC, want map[string][]string) error {
	var err error
	if len(want) == 0 {
		if len(got.GetPolicies()) != 0 {
			err = multierror.Append(err,
				fmt.Errorf("got %d rules but want 0", len(got.Policies)))
		}
	} else {
		if got.Action != envoy_rbac.RBAC_ALLOW {
			err = multierror.Append(err,
				fmt.Errorf("got action %s but want %s", got.GetAction(), envoy_rbac.RBAC_ALLOW))
		}
		if len(want) != len(got.Policies) {
			err = multierror.Append(err, fmt.Errorf("got %d rules but want %d",
				len(got.Policies), len(want)))
		}
		for key, values := range want {
			actual, ok := got.Policies[key]
			actualStr := spew.Sdump(actual)
			if !ok {
				err = multierror.Append(err, fmt.Errorf("not found rule %q", key))
			} else {
				for _, value := range values {
					if !strings.Contains(actualStr, value) {
						err = multierror.Append(err, fmt.Errorf("not found %q in rule %q", value, key))
					}
				}
			}
		}
	}

	return err
}
