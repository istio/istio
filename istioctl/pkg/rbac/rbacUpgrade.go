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

/*
This file upgrades Authorization policy files from v1 to v2.
It works as expected. However, the current version has some limitations:
* It does not support multiple rule in a single ServiceRole yet. TODO(pitlv2109)
* It does not support key value pairs that are not defined in the same line.
* It does not automatically detect if the file is using tabs or spaces for indentation. It assumes
  the file uses spaces for now. TODO(pitlv2109): Nice to have
* It ignores lines that have the comment sign (#). TODO(pitlv2109): Nice to support comments
* It works well when yaml files follow Istio conventions. For example,
  rules:
  - services: ["productpage.svc.cluster.local"]
  but not (some editors auto-indent)
  rules:
    - services: ["productpage.svc.cluster.local"]
*/

package rbac

import (
	"fmt"
	"io/ioutil"
	"os"
	"regexp"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/model"
)

type WorkloadLabels map[string]string

type Upgrader struct {
	IstioConfigStore     model.ConfigStore
	K8sClient            *kubernetes.Clientset
	RoleToWorkloadLabels map[string]WorkloadLabels
	RbacFiles            []string
	serviceRoles         []string
	serviceRoleBindings  []string
}

// Indentation levels.
// It does not automatically detect if the file is using tabs or spaces for indentation. It assumes
// the file uses spaces for now.
// TODO(pitlv2109): Detect indentation type and make the indentation levels configurable.
const (
	// oneLevelIndentation is for fields like namespace, rules.
	oneLevelIndentation = "  "
	// twoLevelsIndentation is for fields like methods, services.
	twoLevelsIndentation = oneLevelIndentation + oneLevelIndentation
)

// RegEx definitions.
const (
	// Matches the services field in ServiceRole that has dash (-) as prefix.
	regExServicesFieldWithPrefixDash = `-.+services: .+\n`
	// Matches the services field in ServiceRole.
	regExServicesField = `services: .+\n`
	// Matches methods or paths field in ServiceRole.
	regExMethodsOrPathsField = `(methods: .+)|(paths: .+)`
	// Matches name field in the metadata section.
	regExNameField = `name: .+\n`
	// Matches namespace field in the metadata section.
	regExNamespaceField = `namespace: .+\n`
	// Matches the roleRef bundle (kind and name) in ServiceRoleBinding.
	regExRoleRefBundleFields = `roleRef:\n(.*)kind: ServiceRole\n(.*)name: (.*)\n`
	// Matches strings inside the quotation marks.
	regExStringInsideQuotationMarks = `("(.*)")|('(.*)')`
	// Matches user field in ServiceRoleBinding. Has ^ to make sure it's not something like
	// request.auth.principal: "user: "
	regExUserInBinding = `(^-.+user: .+)|(^user: .+)`
	// Matches group field in ServiceRoleBinding.
	regExGroupInBinding = `(^-.+group: .+)|(^group: .+)`

	regExYamlFileChecker = `(?i).+\.yaml`
)

const (
	newFileNameSuffix = "-v2"
	threeDashes       = "---"
	commentSign       = "#"
	// yamlListIndicator is the syntax to indicate the first element of a list in YAML.
	yamlListIndicator = "- "

	roleKind        = "kind: ServiceRole"
	bindingKind     = "kind: ServiceRoleBinding"
	authzPolicyKind = "kind: AuthorizationPolicy"

	// Common policy fields.
	specField = "spec:"
	// Metadata section.
	nameField = "name:"

	// ServiceRole section.
	methodsField = "methods:"
	pathsField   = "paths:"

	// ServiceRoleBinding section.
	subjectsField         = "subjects:"
	workloadSelectorField = "workload_selector:"
	allowField            = "allow:"
	userField             = "user:"
	groupField            = "group:"
	namesField            = "names:"
	groupsField           = "groups:"
	roleRefField          = "roleRef:"
	roleField             = "role:"

	methodsAllAccessWithPrefixDash = `- methods: ["*"]`
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
		_, err = newFile.WriteString(role)
		if err != nil {
			return "", err
		}
	}
	for _, serviceRoleBinding := range ug.serviceRoleBindings {
		authzPolicy := ug.createAuthorizationPolicyFromRoleBinding(serviceRoleBinding)
		_, err = newFile.WriteString(authzPolicy)
		if err != nil {
			return "", err
		}
	}
	fmt.Println(fmt.Sprintf("New authorization policies have been upgraded to %s", newFilePath))
	return newFilePath, nil
}

// createRoleAndBindingLists creates lists of strings represent ServiceRole and ServiceRoleBinding policies.
func (ug *Upgrader) createRoleAndBindingLists(fileName string) error {
	rbacFileBuf, err := ioutil.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("failed to read file %s", fileName)
	}
	rbacFileContent := string(rbacFileBuf)
	var stringBuilder strings.Builder
	// Build a string from the file content but ignore lines with the comment sign.
	for _, line := range strings.Split(rbacFileContent, "\n") {
		if !strings.Contains(line, commentSign) {
			stringBuilder.WriteString(fmt.Sprintf("%s\n", line))
		}
	}
	// Check if the last three characters are "---". If not, add them for regex to work nicely.
	if !strings.HasSuffix(stringBuilder.String(), threeDashes) {
		// Add a newline to the file content string if it does not have one at the end.
		if !strings.HasSuffix(stringBuilder.String(), "\n") {
			stringBuilder.WriteString("\n")
		}
		stringBuilder.WriteString(threeDashes)
	}
	rbacFileContent = stringBuilder.String()
	// Get all policies, each ends with ---
	r := regexp.MustCompile(threeDashes)
	matchedDashesIndexes := r.FindAllStringIndex(rbacFileContent, -1)
	startingIdx := 0
	for _, dashesIdx := range matchedDashesIndexes {
		endingIdx := dashesIdx[len(dashesIdx)-1]
		currentPolicy := rbacFileContent[startingIdx:endingIdx]
		if strings.Contains(currentPolicy, bindingKind) {
			ug.serviceRoleBindings = append(ug.serviceRoleBindings, currentPolicy)
		} else if strings.Contains(currentPolicy, roleKind) {
			ug.serviceRoles = append(ug.serviceRoles, currentPolicy)
		}
		startingIdx = dashesIdx[len(dashesIdx)-1] + 1
	}
	return nil
}

// updateRoleToWorkloadLabels maps the ServiceRole name to workload labels that its rules originally
// applies to.
func (ug *Upgrader) updateRoleToWorkloadLabels(serviceRolePolicy string) error {
	roleName := getRoleName(serviceRolePolicy)
	namespace := getNamespace(serviceRolePolicy)
	serviceFieldRegEx := regexp.MustCompile(regExServicesField)
	servicesField := serviceFieldRegEx.FindString(serviceRolePolicy)
	serviceNameRegEx := regexp.MustCompile(regExStringInsideQuotationMarks)
	for _, service := range serviceNameRegEx.FindAllString(servicesField, -1) {
		fullServiceName := service[1 : len(service)-1]
		if err := ug.mapRoleToWorkloadLabels(roleName, namespace, fullServiceName); err != nil {
			return err
		}
	}
	return nil
}

// TODO(pitlv2109): Handle multiple rules.
// upgradeServiceRole simply removes the `services` field for the serviceRolePolicy.
func (ug *Upgrader) upgradeServiceRole(serviceRolePolicy string) string {
	/*
		If services has prefix "-"
			If it's the only first field:
				Create "- methods: ["*"]" to replace the services field
			If it's not the only field:
				Find an equivalent first class field and prepend - to it to replace the services field
		Else:
			Simply ignore/remove it
	*/
	servicesFieldRegEx := regexp.MustCompile(regExServicesField)
	fields := strings.Split(serviceRolePolicy, "\n")
	var stringBuilder strings.Builder
	r := regexp.MustCompile(regExServicesFieldWithPrefixDash)
	// Services field has prefix dash.
	if r.MatchString(serviceRolePolicy) {
		anotherField := getAnotherFirstClassField(serviceRolePolicy)
		// Services is the only first class field.
		if anotherField == "" {
			for _, field := range fields {
				fieldWithNewLine := fmt.Sprintf("%s\n", field)
				// Check if the current fieldWithNewLine is the services field. If so, replace it with
				// - methods: ["*"]
				if servicesFieldRegEx.MatchString(fieldWithNewLine) {
					fieldWithNewLine = fmt.Sprintf("%s%s\n", oneLevelIndentation, methodsAllAccessWithPrefixDash)
				}
				stringBuilder.WriteString(fieldWithNewLine)
			}
		} else { // At least one of `methods` or `paths` exists.
			anotherField = strings.TrimSpace(anotherField)
			anotherField := fmt.Sprintf("%s- %s\n", oneLevelIndentation, anotherField)
			anotherFieldType := methodsField
			if strings.Contains(anotherField, pathsField) {
				anotherFieldType = pathsField
			}
			doneReplaced := false // Check if we're finished replacing the `services` field.
			for _, field := range fields {
				fieldWithNewLine := fmt.Sprintf("%s\n", field)
				// Ignore the `methods` or `paths` field because it has replaced the `services` field's line.
				if doneReplaced && strings.Contains(fieldWithNewLine, anotherFieldType) {
					continue
				}
				// Replace the `services` field.
				if servicesFieldRegEx.MatchString(fieldWithNewLine) {
					fieldWithNewLine = anotherField
					doneReplaced = true
				}
				stringBuilder.WriteString(fieldWithNewLine)
			}
		}
	} else { // `services` field does not have the prefix dash.
		for _, field := range fields {
			fieldWithNewLine := fmt.Sprintf("%s\n", field)
			if !servicesFieldRegEx.MatchString(fieldWithNewLine) {
				stringBuilder.WriteString(fieldWithNewLine)
			}
		}
	}
	return stringBuilder.String()
}

// createAuthorizationPolicyFromRoleBinding creates AuthorizationPolicy from the given ServiceRoleBinding.
// In particular:
// * For each ServiceRoleBinding, create an AuthorizationPolicy
//   * If field `user` exists, change it to use `names` TODO(pitlv2109)
//	 * If field `group` exists, change it to use `groups` TODO(pitlv2109)
//   * Change `roleRef` to use `role`
//   *
func (ug *Upgrader) createAuthorizationPolicyFromRoleBinding(serviceRoleBinding string) string {
	serviceRole := getServiceRoleFromBinding(serviceRoleBinding)
	isFieldUnderSpec := false
	var stringBuilder strings.Builder
	fields := strings.Split(serviceRoleBinding, "\n")
	for _, field := range fields {
		// Change kind from ServiceRoleBinding to AuthorizationPolicy
		if field == bindingKind {
			field = authzPolicyKind
		}
		// Add - to subjects to indicate that it's an element of the list of subjects
		if strings.Contains(field, subjectsField) {
			field = strings.TrimSpace(field)
			field = yamlListIndicator + field
		}
		if strings.Contains(field, userField) || strings.Contains(field, groupField) {
			trimmedField := strings.TrimSpace(field)
			// Check if this is actually a user or group field in binding or it's just something like
			// request.auth.audiences: "my_user:"
			if regexp.MustCompile(regExUserInBinding).MatchString(trimmedField) {
				field = replaceUserOrGroupField(userField, trimmedField)
			} else if regexp.MustCompile(regExGroupInBinding).MatchString(trimmedField) {
				field = replaceUserOrGroupField(groupField, trimmedField)
			}
		}
		// Since we add `allow` field AuthorizationPolicy, we need to increase an indentation level to
		// their existing indentation level for every field under `spec`.
		if isFieldUnderSpec {
			field = oneLevelIndentation + field
		}
		// Create workload selector and allow fields when we see the spec field.
		if field == specField {
			workloadSelectorStr := ""
			if foundWorkloadLabel, found := ug.RoleToWorkloadLabels[serviceRole]; found {
				workloadLabel := mapToString(foundWorkloadLabel)
				workloadSelectorStr = fmt.Sprintf("%s%s\n%s%s", oneLevelIndentation,
					workloadSelectorField, twoLevelsIndentation, workloadLabel)
			}
			field = fmt.Sprintf("%s\n%s%s%s", specField, workloadSelectorStr,
				oneLevelIndentation, allowField)
			// Set this to true so every field after the spec field can be indented properly.
			isFieldUnderSpec = true
		}
		// Replace roleRef bundle with role and finish the conversion process.
		if strings.Contains(field, roleRefField) {
			field = fmt.Sprintf("%s%s %s\n%s", twoLevelsIndentation, roleField, serviceRole, threeDashes)
			stringBuilder.WriteString(field)
			break
		}
		stringBuilder.WriteString(fmt.Sprintf("%s\n", field))
	}
	return stringBuilder.String()
}

// mapRoleToWorkloadLabels maps the roleName from namespace to workload labels that its rules originally
// applies to.
func (ug *Upgrader) mapRoleToWorkloadLabels(roleName, namespace, fullServiceName string) error {
	if strings.Contains(fullServiceName, "*") {
		return fmt.Errorf("services with wildcard * are not supported")
	}
	// TODO(pitlv2109): Handle when services = "*" or wildcards.
	if _, found := ug.RoleToWorkloadLabels[roleName]; found {
		return nil
	}
	serviceName := strings.Split(fullServiceName, ".")[0]
	k8sService, err := ug.K8sClient.CoreV1().Services(namespace).Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	ug.RoleToWorkloadLabels[roleName] = k8sService.Spec.Selector
	return nil
}

// replaceUserOrGroupField convert user and group fields into equivalent names and groups fields.
func replaceUserOrGroupField(fieldType, field string) string {
	// Extract data from `user` or `group` field and trim all spaces left.
	dash := ""
	indentationLevel := twoLevelsIndentation
	// If the fieldType has -, which means it's the first element in the binding list. Therefore,
	// we need to reduce the indentation.
	if string(field[0]) == "-" {
		dash = "- "
		indentationLevel = oneLevelIndentation
	}
	extractedData := strings.TrimSpace(field[len(fmt.Sprintf("%s%s", dash, fieldType)):])
	var newFieldType string
	if fieldType == userField {
		newFieldType = namesField
	} else {
		// Should be safe to not check for fieldType == group:
		newFieldType = groupsField
	}
	return fmt.Sprintf("%s%s%s [%s]", indentationLevel, dash, newFieldType, extractedData)
}

// getAnotherFirstClassField returns either the `methods` field or `paths` field if they exist.
// Return an empty string if neither exists.
func getAnotherFirstClassField(serviceRolePolicy string) string {
	fields := strings.Split(serviceRolePolicy, "\n")
	for _, field := range fields {
		r := regexp.MustCompile(regExMethodsOrPathsField)
		if r.MatchString(field) {
			return field
		}
	}
	return ""
}

// getRoleName returns the ServiceRole name from the name field in the metadata.
func getRoleName(serviceRolePolicy string) string {
	return getValueInMetadata(regExNameField, serviceRolePolicy)
}

// getNamespace returns the ServiceRole namespace from the namespace field in the metadata.
func getNamespace(serviceRolePolicy string) string {
	return getValueInMetadata(regExNamespaceField, serviceRolePolicy)
}

// getServiceRoleFromBinding returns the ServiceRole that it's referring to from roleRef's name.
func getServiceRoleFromBinding(serviceRoleBinding string) string {
	r := regexp.MustCompile(regExRoleRefBundleFields)
	roleRefGroup := r.FindString(serviceRoleBinding)
	for _, field := range strings.Split(roleRefGroup, "\n") {
		if strings.Contains(field, nameField) {
			field = strings.TrimSpace(field)
			// Remove `name: ` and get the name only.
			field = field[len(nameField):]
			// Trim space once more time to remove the space(s) between `name:` and the name.
			field = strings.TrimSpace(field)
			// Return name without the quotation marks.
			if strings.HasPrefix(field, `"`) || strings.HasPrefix(field, `'`) {
				return field[1 : len(field)-1]
			}
			return field
		}
	}
	return ""
}

// mapToString returns a string representation of a map[string]string.
// If the map is key1: value1, key2: value2, the string will be
// key1: value1
// key2: value2
func mapToString(labelMap map[string]string) string {
	ret := ""
	for key, value := range labelMap {
		ret += fmt.Sprintf("%s: %s\n", key, value)
	}
	return ret
}

// getValueInMetadata returns a value in the metadata, for example, it can return the ServiceRole name
// in the name field.
func getValueInMetadata(regexRule, serviceRolePolicy string) string {
	r := regexp.MustCompile(regexRule)
	field := r.FindString(serviceRolePolicy)
	field = strings.Split(field, " ")[1]
	field = strings.TrimSpace(field)
	return strings.TrimSuffix(field, "\n")
}

// checkAndCreateNewFile check if the name of the provided file is good, then create a new file with the
// suffix.
func checkAndCreateNewFile(fileName string) (*os.File, string, error) {
	fileRegEx := regexp.MustCompile(regExYamlFileChecker)
	if !fileRegEx.MatchString(fileName) {
		return nil, "", fmt.Errorf("%s is not a valid YAML file", fileName)
	}
	yamlExtensions := ".yaml"
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
