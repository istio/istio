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
	"strconv"
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

type Upgrader struct {
	K8sClient    *kubernetes.Clientset
	ServiceFiles []string
	// NamespaceToServiceToWorkloadLabels maps the namespace to service name to the workload labels
	// that the service in this namespace pointing to.
	NamespaceToServiceToWorkloadLabels map[string]ServiceToWorkloadLabels
	V1PolicyFile                       string
	serviceRoles                       []model.Config
	serviceRoleBindings                []model.Config
	ConvertedPolicies                  strings.Builder
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
func (ug *Upgrader) UpgradeCRDs() error {
	err := ug.createRoleAndBindingLists(ug.V1PolicyFile)
	if err != nil {
		return err
	}
	// Need to perform the conversion on ServiceRoleBinding first, since ServiceRoles will be modified.
	// This prevents creating unnecessary deep copies of ServiceRoles.
	for _, serviceRoleBinding := range ug.serviceRoleBindings {
		err := ug.createAuthorizationPolicyFromRoleBinding(serviceRoleBinding)
		if err != nil {
			return err
		}
	}
	for _, serviceRole := range ug.serviceRoles {
		err := ug.upgradeServiceRole(serviceRole)
		if err != nil {
			return err
		}
	}
	return nil
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
		if config.Type == model.ServiceRole.Type {
			ug.serviceRoles = append(ug.serviceRoles, config)
		} else if config.Type == model.ServiceRoleBinding.Type {
			ug.serviceRoleBindings = append(ug.serviceRoleBindings, config)
		}
	}
	return nil
}

// upgradeServiceRole writes N new ServiceRoles if there are N rules in serviceRolePolicy.
// Before writing, it also removes the services field in each rule and add methods = ["*"] if necessary.
func (ug *Upgrader) upgradeServiceRole(serviceRolePolicy model.Config) error {
	serviceRoleSpec := serviceRolePolicy.Spec.(*rbacproto.ServiceRole)
	for i, rule := range serviceRoleSpec.Rules {
		// newServiceRole is an individual ServiceRole broken from serviceRolePolicy because it has multiple
		// rules.
		newServiceRole := rbacproto.ServiceRole{}
		newServiceRole.Rules = []*rbacproto.AccessRule{rule}
		if rule.Methods == nil && rule.Paths == nil && rule.Constraints == nil {
			// If `services` is the only field, we need to create `methods = ["*"]`
			newServiceRole.Rules[0].Methods = []string{"*"}
		}
		newServiceRole.Rules[0].Services = nil
		serviceRoleConfig := model.Config{
			ConfigMeta: serviceRolePolicy.ConfigMeta,
			Spec:       &newServiceRole,
		}
		serviceRoleConfig.Name = getNewRoleName(serviceRoleConfig.Name, len(serviceRoleSpec.Rules), i)
		convertedRole, err := parseConfigToString(serviceRoleConfig)
		if err != nil {
			return err
		}
		ug.ConvertedPolicies.WriteString(convertedRole)
	}
	return nil
}

// createAuthorizationPolicyFromRoleBinding creates AuthorizationPolicy from the given ServiceRoleBinding.
// In particular:
// * For each service in the referred ServiceRole from the ServiceRoleBinding, create an AuthorizationPolicy
//   * If field `user` exists, change it to use `names`
//	 * If field `group` exists, change it to use `groups`
//   * Change `roleRef` to use `role`
//   * Create a list of binding with one element (serviceRoleBinding) and use workload selector.
func (ug *Upgrader) createAuthorizationPolicyFromRoleBinding(serviceRoleBinding model.Config) error {
	bindingSpec := serviceRoleBinding.Spec.(*rbacproto.ServiceRoleBinding)
	if bindingSpec.RoleRef == nil {
		return fmt.Errorf("serviceRoleBinding %q does not have `roleRef` field", serviceRoleBinding.Name)
	}
	oldRoleName := bindingSpec.RoleRef.Name
	refRole := ug.getServiceRole(bindingSpec.RoleRef.Name, serviceRoleBinding.Namespace)
	if refRole == nil {
		return fmt.Errorf("cannot find ServiceRole %q from ServiceRoleBinding %q", bindingSpec.RoleRef.Name, serviceRoleBinding.Name)
	}
	refRoleSpec := refRole.Spec.(*rbacproto.ServiceRole)
	for i, rule := range refRoleSpec.Rules {
		newRoleName := getNewRoleName(oldRoleName, len(refRoleSpec.Rules), i)
		for j, serviceName := range rule.Services {
			newAuthzPolicyName := getNewAuthzPolicyName(serviceRoleBinding.Name, len(refRoleSpec.Rules), i, len(rule.Services), j)
			authzPolicy, err := ug.constructAuthorizationPolicy(serviceRoleBinding, serviceName, newRoleName, newAuthzPolicyName)
			if err != nil {
				return err
			}
			convertedAuthzPolicy, err := parseConfigToString(authzPolicy)
			if err != nil {
				return err
			}
			ug.ConvertedPolicies.WriteString(convertedAuthzPolicy)
		}
	}
	return nil
}

func (ug *Upgrader) constructAuthorizationPolicy(serviceRoleBinding model.Config, serviceName, newRoleName, newAuthzPolicyName string) (model.Config, error) {
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
		bindingSpec.RoleRef = nil
		bindingSpec.Role = newRoleName
	}
	authzPolicy := rbacproto.AuthorizationPolicy{}
	authzPolicy.Allow = []*rbacproto.ServiceRoleBinding{bindingSpec}
	workloadLabels, err := ug.getAndAddServiceToWorkloadLabelMapping(serviceName, serviceRoleBinding.Namespace)
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
			Name:      newAuthzPolicyName,
			Namespace: serviceRoleBinding.Namespace,
		},
		Spec: &authzPolicy,
	}
	return authzPolicyConfig, nil
}

// getAndAddServiceToWorkloadLabelMapping get the workload labels given the fullServiceName and namespace. It may
// update the NamespaceToServiceToWorkloadLabels map along the way if the data is not yet there.
func (ug *Upgrader) getAndAddServiceToWorkloadLabelMapping(fullServiceName, namespace string) (WorkloadLabels, error) {
	// TODO: Handle suffix and prefix for services.
	if fullServiceName == "*" {
		return nil, nil
	}
	if strings.Contains(fullServiceName, "*") {
		return nil, fmt.Errorf("wildcard * as prefix or suffix is not supported yet")
	}
	serviceName := strings.Split(fullServiceName, ".")[0]

	if _, found := ug.NamespaceToServiceToWorkloadLabels[namespace][serviceName]; !found {
		// If NamespaceToServiceToWorkloadLabels does not have serviceName key yet, try fetching from Kubernetes apiserver.
		err := ug.addServiceToWorkloadLabelMapping(namespace, serviceName)
		if err != nil {
			return nil, err
		}
	}
	return ug.NamespaceToServiceToWorkloadLabels[namespace][serviceName], nil
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

// addServiceToWorkloadLabelMapping maps the namespace and service name to workload labels that its rules originally
// applies to.
func (ug *Upgrader) addServiceToWorkloadLabelMapping(namespace, serviceName string) error {
	if _, found := ug.NamespaceToServiceToWorkloadLabels[namespace][serviceName]; found {
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
		return fmt.Errorf("no service found in namespace %s", namespace)
	}
	if service.Spec.Selector == nil {
		return fmt.Errorf("failed because service %q does not have selector", serviceName)
	}
	// Maps need to be initialized (from lowest level outwards) before we can write to them.
	if _, found := ug.NamespaceToServiceToWorkloadLabels[namespace][serviceName]; !found {
		if _, found := ug.NamespaceToServiceToWorkloadLabels[namespace]; !found {
			ug.NamespaceToServiceToWorkloadLabels[namespace] = make(ServiceToWorkloadLabels)
		}
		ug.NamespaceToServiceToWorkloadLabels[namespace][serviceName] = make(WorkloadLabels)
	}
	ug.NamespaceToServiceToWorkloadLabels[namespace][serviceName] = service.Spec.Selector
	return nil
}

// getNewRoleName returns a new name for the ServiceRole. If a ServiceRole has more than one rules,
// the new name will be oldName-ruleIndex
func getNewRoleName(oldRoleName string, lenRules, ruleIdx int) string {
	newRoleName := oldRoleName
	if lenRules > 1 {
		newRoleName = fmt.Sprintf("%s-%s", newRoleName, strconv.Itoa(ruleIdx))
	}
	return newRoleName
}

// getNewAuthzPolicyName returns a new name for the AuthorizationPolicy from ServiceRoleBinding.
// The new name follow the format oldBindingName-lenRules-lenServices.
// If lenRules or lenServices is 0, they won't appear in the new name.
func getNewAuthzPolicyName(oldBindingName string, lenRules, ruleIdx, lenServices, svcIdx int) string {
	newAuthzPolicyName := oldBindingName
	if lenRules > 1 {
		newAuthzPolicyName = fmt.Sprintf("%s-%s", newAuthzPolicyName, strconv.Itoa(ruleIdx))
	}
	if lenServices > 1 {
		newAuthzPolicyName = fmt.Sprintf("%s-%s", newAuthzPolicyName, strconv.Itoa(svcIdx))
	}
	return newAuthzPolicyName
}
