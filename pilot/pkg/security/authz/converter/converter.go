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

package converter

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"sort"
	"strings"
	"text/template"

	"github.com/ghodss/yaml"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"

	"istio.io/api/mesh/v1alpha1"
	rbac_v1alpha1 "istio.io/api/rbac/v1alpha1"
	rbac_v1beta1 "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schemas"
	"istio.io/pkg/log"
)

// WorkloadLabels is the workload labels, for example, app: productpage.
type WorkloadLabels map[string]string

// ServiceToWorkloadLabels maps the short service name to the workload labels that it's pointing to.
// This service is defined in same namespace as the ServiceRole that's using it.
type ServiceToWorkloadLabels map[string]WorkloadLabels

type Converter struct {
	v1alpha1Policies             *model.AuthorizationPolicies
	RootNamespace                string
	NamespaceToServiceToSelector map[string]ServiceToWorkloadLabels
	v1beta1Policies              []model.Config
	ConvertedPolicies            strings.Builder
}

const (
	// Properties that are promoted to first class fields.
	sourceNamespace      = "source.namespace"
	sourceIP             = "source.ip"
	requestAuthPrincipal = "request.auth.principal"
)

const (
	rbacNamespaceAllow = `apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: {{ .ScopeName }}-allow-all
  namespace: {{ .Namespace }}
spec:
  rules:
    - {}
---
`
	rbacNamespaceDeny = `apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: {{ .ScopeName }}-deny-all
  namespace: {{ .Namespace }}
spec:
  {}
---
`
)

const (
	istioConfigMapKey = "mesh"
)

func New(k8sClient *kubernetes.Clientset, v1alpha1Policies *model.AuthorizationPolicies, serviceFiles []string,
	meshConfigFile, istioNamespace, meshConfigMapName string) (*Converter, error) {
	var rootNamespace string
	if v1alpha1Policies.RootNamespace != "" {
		rootNamespace = v1alpha1Policies.RootNamespace
	} else {
		var err error
		rootNamespace, err = getRootNamespace(k8sClient, meshConfigFile, meshConfigMapName, istioNamespace)
		if err != nil {
			log.Warnf("failed to get root namespace: %v", err)
		}
	}

	namespaceToServiceToSelector, err := getNamespaceToServiceToSelector(k8sClient, serviceFiles, v1alpha1Policies.ListV1alpha1Namespaces())
	if err != nil {
		log.Warnf("failed to get services: %v", err)
	}

	converter := Converter{
		v1alpha1Policies:             v1alpha1Policies,
		RootNamespace:                rootNamespace,
		NamespaceToServiceToSelector: namespaceToServiceToSelector,
		v1beta1Policies:              []model.Config{},
	}
	return &converter, nil
}

// getRootNamespace returns the root namespace configured in the MeshConfig.
func getRootNamespace(k8sClient *kubernetes.Clientset, meshConfigFile, meshConfigMapName, istioNamespace string) (string, error) {
	var meshConfig *v1alpha1.MeshConfig
	var err error
	if meshConfigFile != "" {
		if meshConfig, err = cmd.ReadMeshConfig(meshConfigFile); err != nil {
			return "", fmt.Errorf("failed to read the provided ConfigMap %s: %s", meshConfigFile, err)
		}
	} else {
		istioConfigMap, err := k8sClient.CoreV1().ConfigMaps(istioNamespace).Get(meshConfigMapName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get ConfigMap named %s in namespace %s. "+
				"Run `kubectl -n %s get configmap %s` to see if it exists", meshConfigMapName, istioNamespace,
				istioNamespace, meshConfigMapName)
		}
		istioMeshConfig, exists := istioConfigMap.Data[istioConfigMapKey]
		if !exists {
			return "", fmt.Errorf("missing ConfigMap key %q", istioConfigMapKey)
		}
		meshConfig, err = mesh.ApplyMeshConfigDefaults(istioMeshConfig)
		if err != nil {
			return "", fmt.Errorf("failed to parse Istio MeshConfig: %s", err)
		}
	}
	return meshConfig.RootNamespace, nil
}

// getNamespaceToServiceToSelector returns the mapping between service and selector.
func getNamespaceToServiceToSelector(k8sClient *kubernetes.Clientset, serviceFiles, namespaces []string) (map[string]ServiceToWorkloadLabels, error) {
	var services []v1.Service
	if len(serviceFiles) != 0 {
		for _, filename := range serviceFiles {
			fileBuf, err := ioutil.ReadFile(filename)
			if err != nil {
				return nil, err
			}
			reader := bytes.NewReader(fileBuf)
			yamlDecoder := kubeyaml.NewYAMLOrJSONDecoder(reader, 512*1024)
			for {
				svc := v1.Service{}
				err = yamlDecoder.Decode(&svc)
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, fmt.Errorf("failed to parse k8s Service file: %s", err)
				}
				services = append(services, svc)
			}
		}
	} else {
		for _, ns := range namespaces {
			rets, err := k8sClient.CoreV1().Services(ns).List(metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			services = append(services, rets.Items...)
		}
	}

	namespaceToServiceToSelector := make(map[string]ServiceToWorkloadLabels)
	for _, svc := range services {
		if len(svc.Spec.Selector) == 0 {
			log.Warnf("ignored service with empty selector: %s.%s", svc.Name, svc.Namespace)
			continue
		}
		if _, found := namespaceToServiceToSelector[svc.Namespace]; !found {
			namespaceToServiceToSelector[svc.Namespace] = make(ServiceToWorkloadLabels)
		}
		namespaceToServiceToSelector[svc.Namespace][svc.Name] = svc.Spec.Selector
	}

	return namespaceToServiceToSelector, nil
}

// ConvertV1alpha1ToV1beta1 converts RBAC v1alphal1 to v1beta1 for local policy files.
func (c *Converter) ConvertV1alpha1ToV1beta1() error {
	if err := c.convert(c.v1alpha1Policies); err != nil {
		return fmt.Errorf("failed to convert policies: %v", err)
	}
	for _, authzPolicy := range c.v1beta1Policies {
		err := c.parseConfigToString(authzPolicy)
		if err != nil {
			return fmt.Errorf("failed to parse config to string: %v", err)
		}
	}
	return nil
}

// convert is the main function that converts RBAC v1alphal1 to v1beta1 for local policy files
func (c *Converter) convert(authzPolicies *model.AuthorizationPolicies) error {
	// Convert ClusterRbacConfig to AuthorizationPolicy
	err := c.convertClusterRbacConfig(authzPolicies)
	if err != nil {
		// Users might not have access to the cluster-wide RBAC config, so instead of returning an error,
		// output a warning instead.
		log.Warnf("failed to convert ClusterRbacConfig: %s", err)
	}
	namespaces := authzPolicies.ListV1alpha1Namespaces()
	if len(namespaces) == 0 {
		// Similarly, a user might want to convert ClusterRbacConfig only.
		log.Warn("no namespace found for ServiceRole and ServiceRoleBinding")
		return nil
	}
	// Build a model for each ServiceRole and associated list of ServiceRoleBinding
	td := trustdomain.NewTrustDomainBundle("cluster.local", nil)
	for _, ns := range namespaces {
		bindingsKeyList := authzPolicies.ListServiceRoleBindings(ns)
		for _, roleConfig := range authzPolicies.ListServiceRoles(ns) {
			roleName := roleConfig.Name
			if bindings, found := bindingsKeyList[roleName]; found {
				m := authz_model.NewModelV1alpha1(td, roleConfig.ServiceRole, bindings)
				err := c.v1alpha1ModelTov1beta1Policy(m, ns)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// convertClusterRbacConfig converts ClusterRbacConfig to AuthorizationPolicy.
func (c *Converter) convertClusterRbacConfig(authzPolicies *model.AuthorizationPolicies) error {
	clusterRbacConfig := authzPolicies.GetClusterRbacConfig()
	if clusterRbacConfig == nil {
		return fmt.Errorf("no ClusterRbacConfig found")
	}
	// RbacConfig_ON_WITH_INCLUSION doesn't require the config root namespace.
	if clusterRbacConfig.Mode == rbac_v1alpha1.RbacConfig_ON_WITH_INCLUSION {
		// Support namespace-level only.
		if len(clusterRbacConfig.Inclusion.Services) > 0 {
			return fmt.Errorf("service-level ClusterRbacConfig (found in ON_WITH_INCLUSION rule) is not supported")
		}
		// For each namespace in RbacConfig_ON_WITH_INCLUSION, we simply generate a deny-all rule for that namespace.
		return c.generateClusterRbacConfig(rbacNamespaceDeny, clusterRbacConfig.Inclusion.Namespaces, false)
	}
	switch clusterRbacConfig.Mode {
	case rbac_v1alpha1.RbacConfig_OFF:
		return c.generateClusterRbacConfig(rbacNamespaceAllow, []string{c.RootNamespace}, true)
	case rbac_v1alpha1.RbacConfig_ON:
		return c.generateClusterRbacConfig(rbacNamespaceDeny, []string{c.RootNamespace}, true)
	case rbac_v1alpha1.RbacConfig_ON_WITH_EXCLUSION:
		// Support namespace-level only.
		if len(clusterRbacConfig.Exclusion.Services) > 0 {
			return fmt.Errorf("service-level ClusterRbacConfig (found in ON_WITH_EXCLUSION rule) is not supported")
		}
		// First generate a cluster-wide deny rule.
		err := c.generateClusterRbacConfig(rbacNamespaceDeny, []string{c.RootNamespace}, true)
		if err != nil {
			return fmt.Errorf("failed to convert ClusterRbacConfig: %v", err)
		}
		// For each namespace in RbacConfig_ON_WITH_EXCLUSION, we simply generate an allow-rule rule for that namespace.
		return c.generateClusterRbacConfig(rbacNamespaceAllow, clusterRbacConfig.Exclusion.Namespaces, false)
	}
	return nil
}

func (c *Converter) generateClusterRbacConfig(template string, namespaces []string, isRootNamespace bool) error {
	clusterRbacConfigData := map[string]string{
		"ScopeName": "",
		"Namespace": "",
	}
	for _, ns := range namespaces {
		clusterRbacConfigData["Namespace"] = ns
		clusterRbacConfigData["ScopeName"] = ns
		if isRootNamespace {
			clusterRbacConfigData["ScopeName"] = "global"
		}
		policy, err := fillTemplate(template, clusterRbacConfigData)
		if err != nil {
			return fmt.Errorf("failed to convert ClusterRbacConfig: %v", err)
		}
		c.ConvertedPolicies.WriteString(policy)
	}
	return nil
}

func fillTemplate(config string, data interface{}) (string, error) {
	tmpl, err := template.New("").Parse(config)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}
	var outString bytes.Buffer
	err = tmpl.Execute(&outString, data)
	if err != nil {
		return "", fmt.Errorf("failed to write template: %v", err)
	}
	return outString.String(), nil
}

// v1alpha1ModelTov1beta1Policy converts the policy of one ServiceRole and a list of associated
// ServiceRoleBinding to the equivalent AuthorizationPolicy.
func (c *Converter) v1alpha1ModelTov1beta1Policy(v1alpha1Model *authz_model.Model, namespace string) error {
	if v1alpha1Model == nil {
		return fmt.Errorf("internal error: No v1alpha1 model")
	}
	if len(v1alpha1Model.Permissions) == 0 {
		return fmt.Errorf("invalid input: ServiceRole has no permissions")
	}
	if len(v1alpha1Model.Principals) == 0 {
		return fmt.Errorf("principals are empty")
	}
	sources, err := convertBindingToSources(v1alpha1Model.Principals)
	if err != nil {
		return fmt.Errorf("cannot convert binding to sources: %v", err)
	}

	createAuthzConfig := func(name string, selector *v1beta1.WorkloadSelector, operation *rbac_v1beta1.Operation) model.Config {
		return model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      schemas.AuthorizationPolicy.Type,
				Name:      name,
				Namespace: namespace,
			},
			Spec: &rbac_v1beta1.AuthorizationPolicy{
				Selector: selector,
				Rules: []*rbac_v1beta1.Rule{
					{
						From: sources,
						To: []*rbac_v1beta1.Rule_To{
							{
								Operation: operation,
							},
						},
					},
				},
			},
		}
	}

	for _, accessRule := range v1alpha1Model.Permissions {
		operation, err := convertAccessRuleToOperation(&accessRule)
		if err != nil {
			return fmt.Errorf("cannot convert access rule to operation: %v", err)
		}

		if len(accessRule.Services) == 0 {
			authzConfig := createAuthzConfig("all-workloads", nil, operation)
			c.v1beta1Policies = append(c.v1beta1Policies, authzConfig)
		}
		for _, service := range accessRule.Services {
			for j, selector := range c.getSelectors(service, namespace) {
				name := fmt.Sprintf("service-%s-%d", strings.ReplaceAll(service, "*", "wildcard"), j)
				authzConfig := createAuthzConfig(name, &v1beta1.WorkloadSelector{
					MatchLabels: selector,
				}, operation)
				c.v1beta1Policies = append(c.v1beta1Policies, authzConfig)
			}
		}
	}
	return nil
}

// getSelector gets the workload label for the service in the given namespace.
func (c *Converter) getSelectors(serviceFullName, namespace string) []WorkloadLabels {
	if serviceFullName == "*" {
		return []WorkloadLabels{
			// An empty workload selects all workloads in the namespace.
			{},
		}
	}

	var serviceName string
	var prefixMatch, suffixMatch bool
	if strings.HasPrefix(serviceFullName, "*") {
		suffixMatch = true
		serviceName = strings.TrimPrefix(serviceFullName, "*")
	} else if strings.HasSuffix(serviceFullName, "*") {
		prefixMatch = true
		serviceName = strings.TrimSuffix(serviceFullName, "*")
	} else {
		serviceName = strings.Split(serviceFullName, ".")[0]
	}

	var selectors []WorkloadLabels

	// Sort the services in the map to make sure the output is stable.
	var targetServices []string
	for svc := range c.NamespaceToServiceToSelector[namespace] {
		targetServices = append(targetServices, svc)
	}
	sort.Strings(targetServices)
	for _, targetService := range targetServices {
		selector := c.NamespaceToServiceToSelector[namespace][targetService]
		if prefixMatch {
			if strings.HasPrefix(targetService, serviceName) {
				selectors = append(selectors, selector)
			}
		} else if suffixMatch {
			if strings.HasSuffix(targetService, serviceName) {
				selectors = append(selectors, selector)
			}
		} else {
			if targetService == serviceName {
				selectors = append(selectors, selector)
			}
		}
	}
	return selectors
}

// TODO(pitlv2109): Handle cases with workload selector from destination.labels and other constraints
// convertAccessRuleToOperation converts one Access Rule to the equivalent Operation.
func convertAccessRuleToOperation(rule *authz_model.Permission) (*rbac_v1beta1.Operation, error) {
	if rule == nil {
		return nil, fmt.Errorf("invalid input: No rule found in ServiceRole")
	}
	operation := rbac_v1beta1.Operation{}
	operation.Methods = rule.Methods
	operation.Paths = rule.Paths
	// TODO(pitlv2109): Handle destination.port
	return &operation, nil
}

// TODO(pitlv2109): Handle properties that are not promoted to first class fields.
// convertBindingToSources converts Subjects to the equivalent Sources.
func convertBindingToSources(principals []authz_model.Principal) ([]*rbac_v1beta1.Rule_From, error) {
	ruleFrom := []*rbac_v1beta1.Rule_From{}
	for _, subject := range principals {
		// TODO(pitlv2109): Handle group
		if subject.Group != "" {
			return nil, fmt.Errorf("serviceRoleBinding with group is not supported")
		}
		from := rbac_v1beta1.Rule_From{
			Source: &rbac_v1beta1.Source{},
		}
		if len(subject.Users) != 0 {
			from.Source.Principals = subject.Users
		}
		if len(subject.Properties) == 0 {
			ruleFrom = append(ruleFrom, &from)
			continue
		}
		// NOTICE: Only select the first element in the properties list.
		for k, v := range subject.Properties[0] {
			switch k {
			case sourceNamespace:
				from.Source.Namespaces = v
			case sourceIP:
				from.Source.IpBlocks = v
			case requestAuthPrincipal:
				from.Source.RequestPrincipals = v
			default:
				return nil, fmt.Errorf("property %s is not supported", k)
			}
		}
		ruleFrom = append(ruleFrom, &from)
	}
	return ruleFrom, nil
}

// parseConfigToString parses data from `config` to string.
func (c *Converter) parseConfigToString(config model.Config) error {
	schema := schemas.AuthorizationPolicy
	obj, err := crd.ConvertConfig(schema, config)
	if err != nil {
		return fmt.Errorf("could not decode %v: %v", config.Name, err)
	}
	configInBytes, err := yaml.Marshal(obj)
	if err != nil {
		return fmt.Errorf("could not marshal %v: %v", config.Name, err)
	}
	configLines := strings.Split(string(configInBytes), "\n")
	for i, configLine := range configLines {
		if i == len(configLines)-1 && configLine == "" {
			c.ConvertedPolicies.WriteString("---\n")
		} else if !strings.Contains(configLine, "creationTimestamp: null") {
			c.ConvertedPolicies.WriteString(fmt.Sprintf("%s\n", configLine))
		}
	}
	return nil
}
