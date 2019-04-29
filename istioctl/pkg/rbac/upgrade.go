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
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/ghodss/yaml"

	"istio.io/istio/pilot/pkg/config/kube/crd"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/model"

	rbacproto "istio.io/api/rbac/v1alpha1"
)

// ServiceToWorkloadLabels maps the short service name to the workload labels that it's pointing to.
// This service is defined in same namespace as the ServiceRole that's using it.
type ServiceToWorkloadLabels map[string]WorkloadLabels

// WorkloadLabels is the workload labels, for example, app: productpage.
type WorkloadLabels map[string]string

type Upgrader struct {
	IstioConfigStore model.ConfigStore
	K8sClient        *kubernetes.Clientset
	// RoleToWorkloadLabels maps the ServiceRole name to the map of service name to the workload labels
	// that the service pointing to.
	// We could have remove the `service` key layer to make it ServiceRole name maps to workload labels.
	// However, this is needed for unit tests.
	RoleToWorkloadLabels map[string]ServiceToWorkloadLabels
	RbacFile             string
	serviceRoles         []model.Config
	serviceRoleBindings  []model.Config
}

const (
	creationTimestampNilField = "creationTimestamp: null"
)

var (
	configDescriptor = model.ConfigDescriptor{
		model.ServiceRole,
		model.ServiceRoleBinding,
		model.AuthorizationPolicy,
	}
)

// UpgradeCRDs is the main function that converts RBAC v1 to v2 for local policy files.
func (ug *Upgrader) UpgradeCRDs() (string, error) {
	err := ug.createRoleAndBindingLists(ug.RbacFile)
	var convertedPolicies strings.Builder
	if err != nil {
		return "", err
	}
	for _, serviceRole := range ug.serviceRoles {
		if err := ug.updateRoleToWorkloadLabels(serviceRole); err != nil {
			return "", err
		}
		role := ug.upgradeServiceRole(serviceRole)
		convertedRole, err := parseConfigToString(role)
		if err != nil {
			return "", err
		}
		convertedPolicies.WriteString(convertedRole)
	}
	for _, serviceRoleBinding := range ug.serviceRoleBindings {
		authzPolicy := ug.createAuthorizationPolicyFromRoleBinding(serviceRoleBinding)
		convertedAuthzPolicy, err := parseConfigToString(authzPolicy)
		if err != nil {
			return "", err
		}
		convertedPolicies.WriteString(convertedAuthzPolicy)
	}
	return convertedPolicies.String(), nil
}

// parseConfigToString parses data from `config` to string.
func parseConfigToString(config model.Config) (string, error) {
	schema, exists := configDescriptor.GetByType(config.Type)
	if !exists {
		return "", fmt.Errorf("unknown kind %q for %v", crd.ResourceName(config.Type), config.Name)
	}
	obj, err := crd.ConvertConfig(schema, config)
	if err != nil {
		return "", fmt.Errorf("could not decode %v: %v", config.Name, err)
	}
	configInBytes, err := yaml.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("could not unmarshal %v: %v", config.Name, err)
	}
	configLines := strings.Split(string(configInBytes), "\n")
	var configInString strings.Builder
	for i, configLine := range configLines {
		if i == len(configLines)-1 && configLine == "" {
			configInString.WriteString("---\n")
		} else if !strings.Contains(configLine, creationTimestampNilField) {
			configInString.WriteString(fmt.Sprintf("%s\n", configLine))
		}
	}
	return configInString.String(), nil
}

// createRoleAndBindingLists creates lists of model.Configs to store ServiceRole and ServiceRoleBinding policies.
func (ug *Upgrader) createRoleAndBindingLists(fileName string) error {
	rbacFileBuf, err := ioutil.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("failed to read file %s", fileName)
	}
	configsFromFile, _, err := crd.ParseInputs(string(rbacFileBuf))
	if err != nil {
		return err
	}
	for _, config := range configsFromFile {
		if config.Type == configDescriptor.Types()[0] {
			ug.serviceRoles = append(ug.serviceRoles, config)
		} else if config.Type == configDescriptor.Types()[1] {
			ug.serviceRoleBindings = append(ug.serviceRoleBindings, config)
		}
	}
	return nil
}

// upgradeServiceRole simply removes the `services` field for the serviceRolePolicy.
func (ug *Upgrader) upgradeServiceRole(serviceRolePolicy model.Config) model.Config {
	serviceRoleSpec := serviceRolePolicy.Spec.(*rbacproto.ServiceRole)
	for _, rule := range serviceRoleSpec.Rules {
		if rule.Methods == nil && rule.Paths == nil {
			// If `services` is the only field, we need to create `methods = ["*"]`
			rule.Methods = []string{"*"}
		}
		rule.Services = nil
	}
	return serviceRolePolicy
}

// createAuthorizationPolicyFromRoleBinding creates AuthorizationPolicy from the given ServiceRoleBinding.
// In particular:
// * For each ServiceRoleBinding, create an AuthorizationPolicy
//   * If field `user` exists, change it to use `names`
//	 * If field `group` exists, change it to use `groups`
//   * Change `roleRef` to use `role`
//   * Create a list of binding with one element (serviceRoleBinding) and use workload selector.
func (ug *Upgrader) createAuthorizationPolicyFromRoleBinding(serviceRoleBinding model.Config) model.Config {
	bindingSpec := serviceRoleBinding.Spec.(*rbacproto.ServiceRoleBinding)
	for _, subject := range bindingSpec.Subjects {
		if subject.User != "" {
			subject.Names = []string{subject.User}
			subject.User = ""
		}
		if subject.Group != "" {
			subject.Groups = []string{subject.Group}
			subject.Group = ""
		}
		if bindingSpec.RoleRef != nil {
			bindingSpec.Role = bindingSpec.RoleRef.Name
			bindingSpec.RoleRef = nil
		}
	}
	authzPolicy := rbacproto.AuthorizationPolicy{}
	authzPolicy.Allow = []*rbacproto.ServiceRoleBinding{bindingSpec}
	workloadLabels := WorkloadLabels{}
	for _, serviceWorkloadLabels := range ug.RoleToWorkloadLabels[bindingSpec.Role] {
		for key, value := range serviceWorkloadLabels {
			workloadLabels[key] = value
		}
	}
	if len(workloadLabels) > 0 {
		authzPolicy.WorkloadSelector = &rbacproto.WorkloadSelector{Labels: workloadLabels}
	}
	authzPolicyConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.AuthorizationPolicy.Type,
			Group:     serviceRoleBinding.Group, // model.AuthorizationPolicy.Group is "rbac", but we need "rbac.istio.io".
			Version:   model.AuthorizationPolicy.Version,
			Name:      serviceRoleBinding.Name,
			Namespace: serviceRoleBinding.Namespace,
		},
		Spec: &authzPolicy,
	}
	return authzPolicyConfig
}

// updateRoleToWorkloadLabels maps the ServiceRole name to workload labels that its rules originally
// applies to.
func (ug *Upgrader) updateRoleToWorkloadLabels(serviceRolePolicy model.Config) error {
	roleName := serviceRolePolicy.Name
	namespace := serviceRolePolicy.Namespace
	serviceRoleSpec := serviceRolePolicy.Spec.(*rbacproto.ServiceRole)
	for _, rule := range serviceRoleSpec.Rules {
		for _, fullServiceName := range rule.Services {
			if err := ug.mapRoleToWorkloadLabels(roleName, namespace, fullServiceName); err != nil {
				return err
			}
		}
	}
	return nil
}

// mapRoleToWorkloadLabels maps the roleName from namespace to workload labels that its rules originally
// applies to.
func (ug *Upgrader) mapRoleToWorkloadLabels(roleName, namespace, fullServiceName string) error {
	// TODO(pitlv2109): Handle when services = "*" or wildcards.
	if strings.Contains(fullServiceName, "*") {
		return fmt.Errorf("services with wildcard * are not supported")
	}
	serviceName := strings.Split(fullServiceName, ".")[0]
	if _, found := ug.RoleToWorkloadLabels[roleName][serviceName]; found {
		return nil
	}
	k8sService, err := ug.K8sClient.CoreV1().Services(namespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if _, found := ug.RoleToWorkloadLabels[roleName]; !found {
		ug.RoleToWorkloadLabels[roleName] = make(ServiceToWorkloadLabels)
	}
	if _, found := ug.RoleToWorkloadLabels[roleName][serviceName]; !found {
		ug.RoleToWorkloadLabels[roleName][serviceName] = make(WorkloadLabels)
	}
	for key, value := range k8sService.Spec.Selector {
		ug.RoleToWorkloadLabels[roleName][serviceName][key] = value
	}
	return nil
}
