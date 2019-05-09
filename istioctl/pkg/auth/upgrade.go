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

package auth

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	v1 "k8s.io/api/core/v1"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/ghodss/yaml"

	"istio.io/istio/pilot/pkg/config/kube/crd"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/model"

	rbacproto "istio.io/api/rbac/v1alpha1"
)

// WorkloadLabels is the workload labels, for example, app: productpage.
type WorkloadLabels map[string]string

// ServiceToWorkloadLabels maps the short service name to the workload labels that it's pointing to.
// This service is defined in same namespace as the ServiceRole that's using it.
type ServiceToWorkloadLabels map[string]WorkloadLabels

type RoleNameToWorkloadLabels map[string]ServiceToWorkloadLabels

type Upgrader struct {
	K8sClient    *kubernetes.Clientset
	ServiceFiles []string
	// NamespaceToRoleNameToWorkloadLabels maps the namespace to role name to a map of service name to the workload labels
	// that the service in this namespace pointing to.
	// We could have remove the `service` key layer to make it ServiceRole name maps to workload labels.
	// However, this is needed for unit tests.
	NamespaceToRoleNameToWorkloadLabels map[string]RoleNameToWorkloadLabels
	V1PolicyFile                        string
	serviceRoles                        []model.Config
	serviceRoleBindings                 []model.Config
}

const (
	creationTimestampNilField = "creationTimestamp: null"
	serviceRoleType           = "service-role"
	serviceRoleBindingType    = "service-role-binding"
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
	err := ug.createRoleAndBindingLists(ug.V1PolicyFile)
	var convertedPolicies strings.Builder
	if err != nil {
		return "", err
	}
	for _, serviceRole := range ug.serviceRoles {
		if err := ug.addRoleNameToWorkloadLabelMapping(serviceRole); err != nil {
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
		authzPolicy, err := ug.createAuthorizationPolicyFromRoleBinding(serviceRoleBinding)
		if err != nil {
			return "", err
		}
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
		if config.Type == serviceRoleType {
			ug.serviceRoles = append(ug.serviceRoles, config)
		} else if config.Type == serviceRoleBindingType {
			ug.serviceRoleBindings = append(ug.serviceRoleBindings, config)
		}
	}
	return nil
}

// upgradeServiceRole simply removes the `services` field for the serviceRolePolicy.
func (ug *Upgrader) upgradeServiceRole(serviceRolePolicy model.Config) model.Config {
	serviceRoleSpec := serviceRolePolicy.Spec.(*rbacproto.ServiceRole)
	for _, rule := range serviceRoleSpec.Rules {
		if rule.Methods == nil && rule.Paths == nil && rule.Constraints == nil {
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
func (ug *Upgrader) createAuthorizationPolicyFromRoleBinding(serviceRoleBinding model.Config) (model.Config, error) {
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
	workloadLabels, err := ug.getAndAddRoleNameToWorkloadLabelMapping(bindingSpec.Role, serviceRoleBinding.Namespace)
	if err != nil {
		return model.Config{}, err
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
	return authzPolicyConfig, nil
}

// getAndAddRoleNameToWorkloadLabelMapping get the workload labels given the role name and namespace. It may
// update the NamespaceToRoleNameToWorkloadLabels map along the way if the data is not yet there.
func (ug *Upgrader) getAndAddRoleNameToWorkloadLabelMapping(roleName, namespace string) (WorkloadLabels, error) {
	workloadLabels := WorkloadLabels{}
	if _, found := ug.NamespaceToRoleNameToWorkloadLabels[namespace][roleName]; !found {
		// If NamespaceToRoleNameToWorkloadLabels does not have roleName key yet, try fetching from Kubernetes apiserver.
		serviceRole := ug.getServiceRole(roleName, namespace)
		if serviceRole == nil {
			return nil, fmt.Errorf("cannot find ServiceRole %q in namespace %q", roleName, namespace)
		}
		err := ug.addRoleNameToWorkloadLabelMapping(*serviceRole)
		if err != nil {
			return nil, err
		}
	}
	// A ServiceRole can have a list of rules. We need to go through all services in all rules for a ServiceRole.
	// A serviceWorkloadLabels is the mapping from pod selector (for example app: productpage).
	for _, serviceWorkloadLabels := range ug.NamespaceToRoleNameToWorkloadLabels[namespace][roleName] {
		// For the example above, labelKey is app, labelValue is productpage.
		for labelKey, labelValue := range serviceWorkloadLabels {
			workloadLabels[labelKey] = labelValue
		}
	}
	return workloadLabels, nil
}

// getServiceRole return the ServiceRole (from the input file) in the provided namespace given the role name.
func (ug *Upgrader) getServiceRole(roleName, namespace string) *model.Config {
	for _, serviceRole := range ug.serviceRoles {
		if serviceRole.Name == roleName && serviceRole.Namespace == namespace {
			return &serviceRole
		}
	}
	return nil
}

// addRoleNameToWorkloadLabelMapping maps the ServiceRole name to workload labels that its rules originally
// applies to.
func (ug *Upgrader) addRoleNameToWorkloadLabelMapping(serviceRolePolicy model.Config) error {
	roleName := serviceRolePolicy.Name
	namespace := serviceRolePolicy.Namespace
	serviceRoleSpec := serviceRolePolicy.Spec.(*rbacproto.ServiceRole)
	for _, rule := range serviceRoleSpec.Rules {
		for _, fullServiceName := range rule.Services {
			if err := ug.mapRoleNameToWorkloadLabels(roleName, namespace, fullServiceName); err != nil {
				return err
			}
		}
	}
	return nil
}

// mapRoleNameToWorkloadLabels maps the roleName from namespace to workload labels that its rules originally
// applies to.
func (ug *Upgrader) mapRoleNameToWorkloadLabels(roleName, namespace, fullServiceName string) error {
	// TODO: Handle suffix for services as described at https://istio.io/docs/reference/config/authorization/istio.rbac.v1alpha1/#AccessRule
	if fullServiceName == "*" {
		return nil
	}
	serviceName := strings.Split(fullServiceName, ".")[0]
	if _, found := ug.NamespaceToRoleNameToWorkloadLabels[namespace][roleName][serviceName]; found {
		return nil
	}

	// TODO: cache the Service.
	var service *v1.Service
	if len(ug.ServiceFiles) != 0 {
		svc := v1.Service{}
		for _, filename := range ug.ServiceFiles {
			fileBuf, err := ioutil.ReadFile(filename)
			if err != nil {
				return err
			}
			reader := bytes.NewReader(fileBuf)
			yamlDecoder := kubeyaml.NewYAMLOrJSONDecoder(reader, 512*1024)
			for {
				err = yamlDecoder.Decode(&svc)
				if err == io.EOF {
					break
				}
				if err != nil {
					return fmt.Errorf("failed to parse k8s Service file: %s", err)
				}
				if svc.Name == serviceName && svc.Namespace == namespace {
					service = &svc
					break
				}
			}
		}
	} else {
		var err error
		service, err = ug.K8sClient.CoreV1().Services(namespace).Get(serviceName, metav1.GetOptions{})
		if err != nil {
			return err
		}
	}
	if service == nil {
		return fmt.Errorf("no service found for role %s in namespace %s", roleName, namespace)
	}
	if service.Spec.Selector == nil {
		return fmt.Errorf("failed because service %q does not have selector", serviceName)
	}
	// Maps need to be initialized (from lowest level outwards) before we can write to them.
	if _, found := ug.NamespaceToRoleNameToWorkloadLabels[namespace][roleName][serviceName]; !found {
		if _, found := ug.NamespaceToRoleNameToWorkloadLabels[namespace][roleName]; !found {
			if _, found := ug.NamespaceToRoleNameToWorkloadLabels[namespace]; !found {
				ug.NamespaceToRoleNameToWorkloadLabels[namespace] = make(RoleNameToWorkloadLabels)
			}
			ug.NamespaceToRoleNameToWorkloadLabels[namespace][roleName] = make(ServiceToWorkloadLabels)
		}
		ug.NamespaceToRoleNameToWorkloadLabels[namespace][roleName][serviceName] = make(WorkloadLabels)
	}
	ug.NamespaceToRoleNameToWorkloadLabels[namespace][roleName][serviceName] = service.Spec.Selector
	return nil
}
