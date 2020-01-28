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
	"regexp"
	"strings"

	"github.com/davecgh/go-spew/spew"
	envoyRbacPb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	"github.com/hashicorp/go-multierror"

	istioRbacPb "istio.io/api/rbac/v1alpha1"
	istioSecurityPb "istio.io/api/security/v1beta1"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	authzModel "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/collections"
)

// We cannot import `testing` here, as it will bring extra test flags into the binary. Instead, just include the interface here
type mockTest interface {
	Fatalf(format string, args ...interface{})
	Helper()
}

func NewServiceMetadata(hostname string, labels map[string]string, t mockTest) *authzModel.ServiceMetadata {
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
			Hostname: host.Name(hostname),
		},
		Endpoint: &model.IstioEndpoint{
			Attributes: model.ServiceAttributes{
				Name:      name,
				Namespace: namespace,
			},
			Labels: labels,
		},
	}

	serviceMetadata, err := authzModel.NewServiceMetadata(name, namespace, serviceInstance)
	if err != nil {
		t.Fatalf("failed to initialize service instance: %s", err)
	}

	return serviceMetadata
}

func NewAuthzPolicies(policies []*model.Config, t mockTest) *model.AuthorizationPolicies {
	t.Helper()
	store := model.MakeIstioStore(memory.Make(collections.Pilot))
	for _, p := range policies {
		if _, err := store.Create(*p); err != nil {
			t.Fatalf("failed to initialize authz policies: %s", err)
		}
	}

	authzPolicies, err := model.GetAuthorizationPolicies(&model.Environment{
		IstioConfigStore: store,
	})
	if err != nil {
		t.Fatalf("failed to create authz policies: %s", err)
	}

	return authzPolicies
}

func SimpleClusterRbacConfig() *model.Config {
	cfg := &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().Kind(),
			Version:   collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().Version(),
			Group:     collections.IstioRbacV1Alpha1Clusterrbacconfigs.Resource().Group(),
			Name:      "default",
			Namespace: "default",
		},
		Spec: &istioRbacPb.RbacConfig{
			Mode: istioRbacPb.RbacConfig_ON,
		},
	}
	return cfg
}

func RoleTag(name string) string {
	return fmt.Sprintf("MethodFromRole[%s]", name)
}

func SimpleRole(name string, namespace string, service string) *model.Config {
	spec := &istioRbacPb.ServiceRole{
		Rules: []*istioRbacPb.AccessRule{
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
			Type:      collections.IstioRbacV1Alpha1Serviceroles.Resource().Kind(),
			Version:   collections.IstioRbacV1Alpha1Serviceroles.Resource().Version(),
			Group:     collections.IstioRbacV1Alpha1Serviceroles.Resource().Group(),
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

func CustomPrincipal(trustDomain, namespace, saName string) string {
	return fmt.Sprintf("%s/ns/%s/sa/%s", trustDomain, namespace, saName)
}

func BindingTag(name string) string {
	return fmt.Sprintf("UserFromBinding[%s]", name)
}

func SimpleBinding(name, namespace, role string) *model.Config {
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Kind(),
			Version:   collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Version(),
			Group:     collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Group(),
			Name:      name,
			Namespace: namespace,
		},
		Spec: &istioRbacPb.ServiceRoleBinding{
			Subjects: []*istioRbacPb.Subject{
				{
					User: BindingTag(name),
				},
			},
			RoleRef: &istioRbacPb.RoleRef{
				Name: role,
				Kind: "ServiceRole",
			},
		},
	}
}

func SimpleBindingWithUser(name, namespace, role, user string) *model.Config {
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Kind(),
			Version:   collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Version(),
			Group:     collections.IstioRbacV1Alpha1Servicerolebindings.Resource().Group(),
			Name:      name,
			Namespace: namespace,
		},
		Spec: &istioRbacPb.ServiceRoleBinding{
			Subjects: []*istioRbacPb.Subject{
				{
					User: user,
				},
			},
			RoleRef: &istioRbacPb.RoleRef{
				Name: role,
				Kind: "ServiceRole",
			},
		},
	}
}

func SimplePermissiveBinding(name string, namespace string, role string) *model.Config {
	cfg := SimpleBinding(name, namespace, role)
	binding := cfg.Spec.(*istioRbacPb.ServiceRoleBinding)
	binding.Mode = istioRbacPb.EnforcementMode_PERMISSIVE
	return cfg
}

func AuthzPolicyTag(name string) string {
	return fmt.Sprintf("UserFromPolicy[%s]", name)
}

func SimpleAuthorizationProto(name string, action istioSecurityPb.AuthorizationPolicy_Action) *istioSecurityPb.AuthorizationPolicy {
	return &istioSecurityPb.AuthorizationPolicy{
		Action: action,
		Rules: []*istioSecurityPb.Rule{
			{
				From: []*istioSecurityPb.Rule_From{
					{
						Source: &istioSecurityPb.Source{
							Principals: []string{AuthzPolicyTag(name)},
						},
					},
				},
				To: []*istioSecurityPb.Rule_To{
					{
						Operation: &istioSecurityPb.Operation{
							Methods: []string{"GET"},
						},
					},
				},
			},
		},
	}
}

func SimpleAllowPolicy(name string, namespace string) *model.Config {
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Kind(),
			Version:   collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Version(),
			Group:     collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Group(),
			Name:      name,
			Namespace: namespace,
		},
		Spec: SimpleAuthorizationProto(name, istioSecurityPb.AuthorizationPolicy_ALLOW),
	}
}

func SimpleDenyPolicy(name string, namespace string) *model.Config {
	return &model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Kind(),
			Group:     collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Group(),
			Version:   collections.IstioSecurityV1Beta1Authorizationpolicies.Resource().Version(),
			Name:      name,
			Namespace: namespace,
		},
		Spec: SimpleAuthorizationProto(name, istioSecurityPb.AuthorizationPolicy_DENY),
	}
}

func Verify(got *envoyRbacPb.RBAC, want map[string][]string, needToCheckPrincipals bool, wantDeny bool) error {
	var err error
	if len(want) == 0 {
		if len(got.GetPolicies()) != 0 {
			err = multierror.Append(err,
				fmt.Errorf("got %d rules but want 0", len(got.Policies)))
		}
	} else {
		if (wantDeny && got.Action != envoyRbacPb.RBAC_DENY) || (!wantDeny && got.Action != envoyRbacPb.RBAC_ALLOW) {
			err = multierror.Append(err,
				fmt.Errorf("got action %s but want opposite", got.GetAction()))
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
				// Enable principal count check if principals are important for the current test (e.g. generating RBAC config based
				// on trust domain aliases).
				if needToCheckPrincipals {
					err = checkPrincipalCounts(key, actualStr, values)
				}
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

// checkSourcePrincipalCounts checks if |actual| and |want| have the same number of source.principal.
// For example, both |actual| and |want| can only have one key "source.principal",
// but |actual| can have "foo" and "bar", and |want| can only has "foo". This is possible since
// in v1beta1, principals can be a list.
func checkPrincipalCounts(ruleKey string, actual string, want []string) error {
	// Only work with principals, not other identities like namespaces.
	keyRegEx := regexp.MustCompile("source.principal")
	keyMatches := keyRegEx.FindAllStringIndex(actual, -1)
	numPrincipals := 0
	for _, w := range want {
		if isPrincipal(w) {
			numPrincipals++
		}
	}
	if len(keyMatches) != numPrincipals {
		return fmt.Errorf("the number of principals are not the same in rule %q.\n"+
			"Got %s, want %s", ruleKey, actual, want)
	}
	return nil
}

// isPrincipal returns if s is an Istio authorization principal.
// It supports * in principals (including prefix or suffix in some cases),
// but the principals must be in the format */*/*/*/*.
// This should be used for testing only.
func isPrincipal(s string) bool {
	return s == "*" || len(strings.Split(s, "/")) == 5
}
