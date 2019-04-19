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

package rbac

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	rbacproto "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

const (
	AllNamespaces = "ALL_NAMESPACES"
)

type WorkloadLabels map[string]string

type Upgrader struct {
	IstioConfigStore     model.ConfigStore
	K8sClient            *kubernetes.Clientset
	Namespace            string
	RoleToWorkloadLabels map[string]WorkloadLabels
}

func (ug *Upgrader) UpgradeCRDs() error {
	roles, err := ug.IstioConfigStore.List(model.ServiceRole.Type, ug.Namespace)
	if err != nil {
		return err
	}
	bindings, err := ug.IstioConfigStore.List(model.ServiceRoleBinding.Type, ug.Namespace)
	if err != nil {
		return err
	}
	if err := ug.upgradeServiceRole(roles); err != nil {
		return err
	}
	return ug.deprecateServiceRoleBinding(bindings)
}

// upgradeServiceRole upgrades ServiceRole from RBAC v1 to RBAC v2.
// This will update ServiceRole policies in the mesh.
// In particular, upgrading ServiceRole means:
// * Get the `services` field, look up pod label in the service.
// * Remove the `services` field.
func (ug *Upgrader) upgradeServiceRole(roles []model.Config) error {
	for _, role := range roles {
		spec := role.Spec.(*rbacproto.ServiceRole)
		for _, rule := range spec.Rules {
			for _, service := range rule.Services {
				if err := ug.mapRoleToWorkloadLabels(role.Name, service); err != nil {
					return err
				}
			}
			rule.Services = nil
			_, err := ug.IstioConfigStore.Update(role)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (ug *Upgrader) mapRoleToWorkloadLabels(roleName, fullServiceName string) error {
	// TODO(pitlv2109): Handle when services = "*"
	serviceName := strings.Split(fullServiceName, ".")[0]
	k8sService, err := ug.K8sClient.CoreV1().Services(ug.Namespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	ug.RoleToWorkloadLabels[roleName] = k8sService.Spec.Selector
	return nil
}

// deprecateServiceRoleBinding deprecates ServiceRoleBinding and use AuthorizationPolicy instead.
// This will deprecate ServiceRoleBinding policies in the mesh and create equivalent AuthorizationPolicy.
// In particular, upgrading ServiceRoleBinding means:
// * For each ServiceRoleBinding, create an AuthorizationPolicy
//   * If field `user` exists, change it to use `names`
//   * Change `roleRef` to use `role`
//   *
func (ug *Upgrader) deprecateServiceRoleBinding(bindings []model.Config) error {
	for _, binding := range bindings {
		bindingSpec := binding.Spec.(*rbacproto.ServiceRoleBinding)
		for _, subject := range bindingSpec.Subjects {
			if subject.User != "" {
				subject.Names = []string{subject.User}
				subject.User = ""
			}
		}
		if bindingSpec.RoleRef != nil {
			bindingSpec.Role = bindingSpec.RoleRef.Name
			bindingSpec.RoleRef = nil
		}
		authzPolicyConfig := ug.constructAuthorizationPolicy(&binding, bindingSpec)
		_, err := ug.IstioConfigStore.Create(authzPolicyConfig)
		if err != nil {
			return err
		}
		err = ug.IstioConfigStore.Delete(model.ServiceRoleBinding.Type, binding.Name, ug.Namespace)
		if err != nil {
			return err
		}
	}
	return nil
}

func (ug *Upgrader) constructAuthorizationPolicy(binding *model.Config, bindingSpec *rbacproto.ServiceRoleBinding) model.Config {
	authzPolicy := rbacproto.AuthorizationPolicy{}
	authzPolicy.Allow = []*rbacproto.ServiceRoleBinding{bindingSpec}
	workloadLabels, found := ug.RoleToWorkloadLabels[bindingSpec.Role]
	if found {
		authzPolicy.WorkloadSelector = &rbacproto.WorkloadSelector{Labels: workloadLabels}
	}
	authzPolicyConfig := model.Config{
		ConfigMeta: binding.ConfigMeta,
		Spec:       &authzPolicy,
	}
	// TODO(pitlv2109): Change annotation as well (nice to have).
	authzPolicyConfig.ResourceVersion = ""
	authzPolicyConfig.Type = model.AuthorizationPolicy.Type
	authzPolicyConfig.Group = binding.Group // model.AuthorizationPolicy.Group is "rbac", but we need "rbac.istio.io".
	authzPolicyConfig.Version = model.AuthorizationPolicy.Version
	authzPolicyConfig.ConfigMeta.Type = model.AuthorizationPolicy.Type
	return authzPolicyConfig
}
