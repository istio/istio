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
	"os"
	"strings"

	"gopkg.in/yaml.v2"

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
	RbacFiles            []string
	serviceRoles         []model.Config
	serviceRoleBindings  []model.Config
}

const (
	apiVersionField = "apiVersion: "
	kindField       = "kind: "
	metadataField   = "metadata:"
	nameField       = "name: "
	namespaceField  = "namespace: "
	specField       = "spec:"

	newFileNameSuffix = "-v2"
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
	for _, fileName := range ug.RbacFiles {
		_, err := ug.upgradeLocalFile(fileName)
		if err != nil {
			return fmt.Errorf("failed to convert file %s with error %v", fileName, err)
		}
	}
	return nil
}

// upgradeLocalFile performs upgrade/conversion for all ServiceRole and ServiceRoleBinding in the fileName.
func (ug *Upgrader) upgradeLocalFile(fileName string) (string, error) {
	err := ug.createRoleAndBindingLists(fileName)
	if err != nil {
		return "", err
	}
	newFile, newFilePath, err := checkAndCreateNewFile(fileName)
	if err != nil {
		return "", err
	}
	for _, serviceRole := range ug.serviceRoles {
		if err := ug.updateRoleToWorkloadLabels(serviceRole); err != nil {
			return "", err
		}
		role := ug.upgradeServiceRole(serviceRole)
		err = writeToFile(newFile, role)
		if err != nil {
			return "", err
		}
	}
	for _, serviceRoleBinding := range ug.serviceRoleBindings {
		authzPolicy := ug.createAuthorizationPolicyFromRoleBinding(serviceRoleBinding)
		err = writeToFile(newFile, authzPolicy)
		if err != nil {
			return "", err
		}
	}
	fmt.Println(fmt.Sprintf("New authorization policies have been upgraded to %s", newFilePath))
	return newFilePath, nil
}

// writeToFile writes data from `config` to `file`.
func writeToFile(file *os.File, config model.Config) error {
	var stringBuilder strings.Builder
	schema, exists := configDescriptor.GetByType(config.Type)
	if !exists {
		return fmt.Errorf("unknown kind %q for %v", crd.ResourceName(config.Type), config.Name)
	}
	obj, err := crd.ConvertConfig(schema, config)
	if err != nil {
		return fmt.Errorf("could not decode %v: %v", config.Name, err)
	}
	stringBuilder.WriteString(fmt.Sprintf("%s%s/%s\n", apiVersionField, config.Group, config.Version))
	stringBuilder.WriteString(fmt.Sprintf("%s%s\n", kindField, obj.GetObjectKind().GroupVersionKind().Kind))
	stringBuilder.WriteString(fmt.Sprintf("%s\n", metadataField))
	stringBuilder.WriteString(fmt.Sprintf("  %s%s\n", nameField, config.Name))
	stringBuilder.WriteString(fmt.Sprintf("  %s%s\n", namespaceField, config.Namespace))
	stringBuilder.WriteString(fmt.Sprintf("%s\n", specField))
	spec, err := yaml.Marshal(obj.GetSpec())
	if err != nil {
		return err
	}
	specLines := strings.Split(string(spec), "\n")
	for i, line := range specLines {
		// If specLines has an empty line at the end, ignore it for better formatting.
		if i == len(specLines)-1 && line == "" {
			break
		}
		stringBuilder.WriteString(fmt.Sprintf("  %s\n", line))
	}
	stringBuilder.WriteString("---\n")
	if _, err := file.WriteString(stringBuilder.String()); err != nil {
		return fmt.Errorf("failed to write %s to %s because %v", config.Name, file.Name(), err)
	}
	return nil
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
			// ServiceRole
			ug.serviceRoles = append(ug.serviceRoles, config)
		} else if config.Type == configDescriptor.Types()[1] {
			// ServiceRoleBinding
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

// checkAndCreateNewFile check if the name of the provided file is good, then create a new file with the
// suffix.
func checkAndCreateNewFile(fileName string) (*os.File, string, error) {
	yamlExtensions := ".yaml"
	if !strings.HasSuffix(strings.ToLower(fileName), yamlExtensions) {
		return nil, "", fmt.Errorf("%s is not a valid YAML file", fileName)
	}
	// Remove the .yaml part
	fileName = fileName[:len(fileName)-len(yamlExtensions)]
	newFilePath := fmt.Sprintf("%s%s%s", fileName, newFileNameSuffix, yamlExtensions)
	if _, err := os.Stat(newFilePath); !os.IsNotExist(err) {
		// File already exist, return with error
		return nil, "", fmt.Errorf("%s already exist", newFilePath)
	}
	newFile, err := os.Create(newFilePath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create file: %v", err)
	}
	return newFile, newFilePath, nil
}
