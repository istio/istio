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

	"github.com/ghodss/yaml"
	"github.com/mitchellh/copystructure"

	"istio.io/api/type/v1beta1"

	v1 "k8s.io/api/core/v1"

	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schemas"

	rbac_v1alpha1 "istio.io/api/rbac/v1alpha1"
	rbac_v1beta1 "istio.io/api/security/v1beta1"

	authz_model "istio.io/istio/pilot/pkg/security/authz/model"

	"k8s.io/client-go/kubernetes"

	"istio.io/istio/pilot/pkg/model"
)

// WorkloadLabels is the workload labels, for example, app: productpage.
type WorkloadLabels map[string]string

// ServiceToWorkloadLabels maps the short service name to the workload labels that it's pointing to.
// This service is defined in same namespace as the ServiceRole that's using it.
type ServiceToWorkloadLabels map[string]WorkloadLabels

type Upgrader struct {
	K8sClient                          *kubernetes.Clientset
	V1PolicyFiles                      []string
	ServiceFiles                       []string
	MeshConfigFile                     string
	IstioNamespace                     string
	MeshConfigMapName                  string
	NamespaceToServiceToWorkloadLabels map[string]ServiceToWorkloadLabels
	AuthorizationPolicies              []model.Config
	ConvertedPolicies                  strings.Builder
}

const (
	// Properties that are promoted to first class fields.
	sourceNamespace      = "source.namespace"
	sourceIP             = "source.ip"
	requestAuthPrincipal = "request.auth.principal"
)

const (
	rbacGlobalAllowAll = `apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: allow-all
  namespace: {{ .RootNamespace }}
spec:
  rules:
    - {}
---
`
	rbacGlobalDenyAll = `apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: deny-all
  namespace: {{ .RootNamespace }}
spec:
---
`
)

const (
	istioConfigMapKey = "mesh"
)

func NewUpgrader(k8sClient *kubernetes.Clientset, v1PolicyFiles, serviceFiles []string, meshConfigFile, istioNamespace, meshConfigMapName string) *Upgrader {
	upgrader := Upgrader{
		K8sClient:                          k8sClient,
		V1PolicyFiles:                      v1PolicyFiles,
		ServiceFiles:                       serviceFiles,
		MeshConfigFile:                     meshConfigFile,
		IstioNamespace:                     istioNamespace,
		MeshConfigMapName:                  meshConfigMapName,
		NamespaceToServiceToWorkloadLabels: make(map[string]ServiceToWorkloadLabels),
		AuthorizationPolicies:              []model.Config{},
	}
	return &upgrader
}

// ConvertV1alpha1ToV1beta1 converts RBAC v1alphal1 to v1beta1 for local policy files.
func (ug *Upgrader) ConvertV1alpha1ToV1beta1() error {
	configsFromFile, err := getRbacConfigsFromFiles(ug.V1PolicyFiles)
	if err != nil {
		return fmt.Errorf("failed to get RBAC config from files: %v", err)
	}
	authzPolicies, err := getAuthorizationPolicies(configsFromFile)
	if err != nil {
		return fmt.Errorf("failed to get AuthorizationPolices store: %v", err)
	}
	err = ug.convert(authzPolicies)
	if err != nil {
		return fmt.Errorf("failed to convert policies: %v", err)
	}
	for _, authzPolicy := range ug.AuthorizationPolicies {
		err := ug.parseConfigToString(authzPolicy)
		if err != nil {
			return fmt.Errorf("failed to parse config to string: %v", err)
		}
	}
	return nil
}

// getRbacConfigsFromFiles parses RBAC policy files and store them in an internal data type.
func getRbacConfigsFromFiles(fileNames []string) ([]model.Config, error) {
	configsFromFile := []model.Config{}
	for _, fileName := range fileNames {
		rbacFileBuf, err := ioutil.ReadFile(fileName)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s", fileName)
		}
		configFromFile, _, err := crd.ParseInputs(string(rbacFileBuf))
		if err != nil {
			return nil, err
		}
		configsFromFile = append(configsFromFile, configFromFile...)
	}
	return configsFromFile, nil
}

// getAuthorizationPolicies creates an Istio store for the provided policies and returns all RBAC policy
// in form of AuthorizationPolicies struct.
func getAuthorizationPolicies(policies []model.Config) (*model.AuthorizationPolicies, error) {
	store := model.MakeIstioStore(memory.Make(schemas.Istio))
	for _, p := range policies {
		if _, err := store.Create(p); err != nil {
			return nil, fmt.Errorf("failed to initialize authz policies: %s", err)
		}
	}
	authzPolicies, err := model.GetAuthorizationPolicies(&model.Environment{
		IstioConfigStore: store,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create authz policies: %s", err)
	}
	return authzPolicies, nil
}

// convert is the main function that converts RBAC v1alphal1 to v1beta1 for local policy files
func (ug *Upgrader) convert(authzPolicies *model.AuthorizationPolicies) error {
	// Convert ClusterRbacConfig to AuthorizationPolicy
	err := ug.convertClusterRbacConfig(authzPolicies)
	if err != nil {
		// Users might not have access to the cluster-wide RBAC config, so instead of returning an error,
		// output a warning instead.
		log.Warnf("failed to convert ClusterRbacConfig: %s", err)
	}
	namespaces := authzPolicies.ListNamespacesOfToV1alpha1Policies()
	if len(namespaces) == 0 {
		// Similarly, a user might want to convert ClusterRbacConfig only.
		log.Warn("no namespace found for ServiceRole and ServiceRoleBinding")
		return nil
	}
	// Build a model for each ServiceRole and associated list of ServiceRoleBinding
	for _, ns := range namespaces {
		bindingsKeyList := authzPolicies.ListServiceRoleBindings(ns)
		for _, roleConfig := range authzPolicies.ListServiceRoles(ns) {
			roleName := roleConfig.Name
			if bindings, found := bindingsKeyList[roleName]; found {
				role := roleConfig.Spec.(*rbac_v1alpha1.ServiceRole)
				m := authz_model.NewModelV1alpha1(trustdomain.NewTrustDomainBundle("", nil), role, bindings)
				err := ug.v1alpha1ModelTov1beta1Policy(m, ns)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// convertClusterRbacConfig converts ClusterRbacConfig to AuthorizationPolicy.
func (ug *Upgrader) convertClusterRbacConfig(authzPolicies *model.AuthorizationPolicies) error {
	clusterRbacConfig := authzPolicies.GetClusterRbacConfig()
	if clusterRbacConfig == nil {
		return fmt.Errorf("no ClusterRbacConfig found")
	}
	rootNamespace, err := ug.getIstioRootNamespace()
	if err != nil {
		return fmt.Errorf("failed to get Istio root namespace: %s", err)
	}
	rootNs := map[string]string{
		"RootNamespace": rootNamespace,
	}
	var policy string
	switch clusterRbacConfig.Mode {
	case rbac_v1alpha1.RbacConfig_OFF:
		policy, err = fillTemplate(rbacGlobalAllowAll, rootNs)
	case rbac_v1alpha1.RbacConfig_ON:
		policy, err = fillTemplate(rbacGlobalDenyAll, rootNs)
	}
	if err != nil {
		return fmt.Errorf("failed to convert ClusterRbacConfig: %v", err)
	}
	ug.ConvertedPolicies.WriteString(policy)
	return nil
}

// v1alpha1ModelTov1beta1Policy converts the policy of one ServiceRole and a list of associated
// ServiceRoleBinding to the equivalent AuthorizationPolicy.
func (ug *Upgrader) v1alpha1ModelTov1beta1Policy(v1alpha1Model *authz_model.Model, namespace string) error {
	if v1alpha1Model == nil {
		return fmt.Errorf("internal error: No v1alpha1 model")
	}
	if v1alpha1Model.Permissions == nil || len(v1alpha1Model.Permissions) == 0 {
		return fmt.Errorf("invalid input: ServiceRole has no permissions")
	}
	if v1alpha1Model.Principals == nil || len(v1alpha1Model.Principals) == 0 {
		return fmt.Errorf("principals are empty")
	}
	// TODO(pitlv2109): Support more complex cases
	if len(v1alpha1Model.Permissions) > 1 {
		return fmt.Errorf("more than one access rule is not supported")
	}
	accessRule := v1alpha1Model.Permissions[0]
	operations, err := convertAccessRuleToOperation(&accessRule)
	if err != nil {
		return fmt.Errorf("cannot convert access rule to operation: %v", err)
	}
	sources, err := convertBindingToSources(v1alpha1Model.Principals)
	if err != nil {
		return fmt.Errorf("cannot convert binding to sources: %v", err)
	}
	// If there is no services field or it's defined as "*", it's equivalent to an AuthorizationPolicy
	// with no workload selector
	authzPolicyConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      schemas.AuthorizationPolicy.Type,
			Namespace: namespace,
		},
		Spec: &rbac_v1beta1.AuthorizationPolicy{
			Selector: &v1beta1.WorkloadSelector{},
			Rules: []*rbac_v1beta1.Rule{
				{
					From: sources,
					To: []*rbac_v1beta1.Rule_To{
						{
							Operation: operations,
						},
					},
				},
			},
		},
	}
	if len(accessRule.Services) == 0 || accessRule.Services[0] == "*" {
		authzPolicyConfig.Name = fmt.Sprintf("%s-all", namespace)
		authzPolicyConfig.Spec.(*rbac_v1beta1.AuthorizationPolicy).Selector = nil
		ug.AuthorizationPolicies = append(ug.AuthorizationPolicies, authzPolicyConfig)
		return nil
	}
	for _, service := range accessRule.Services {
		serviceName := strings.Split(service, ".")[0]
		authzPolicyConfig.Name = fmt.Sprintf("%s-%s", namespace, serviceName)
		workloadSelector, err := ug.getAndAddServiceToWorkloadLabelMapping(service, namespace)
		if err != nil {
			return err
		}
		authzPolicyCopy, err := copystructure.Copy(&authzPolicyConfig)
		if err != nil {
			return err
		}
		authzPolicyCopy.(*model.Config).Spec.(*rbac_v1beta1.AuthorizationPolicy).Selector.MatchLabels = workloadSelector
		authzPolicyConfigCopy := authzPolicyCopy.(*model.Config)
		ug.AuthorizationPolicies = append(ug.AuthorizationPolicies, *authzPolicyConfigCopy)
	}
	return nil
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

// getAndAddServiceToWorkloadLabelMapping get the workload labels given the fullServiceName and namespace. It may
// update the NamespaceToServiceToWorkloadLabels map along the way if the data is not yet there.
// fullServiceName is in the form of serviceName.ns.svc.cluster.local
func (ug *Upgrader) getAndAddServiceToWorkloadLabelMapping(fullServiceName, namespace string) (WorkloadLabels, error) {
	if fullServiceName == "*" {
		return nil, nil
	}
	// TODO(pitlv2109): Handle suffix and prefix for services.
	if strings.Contains(fullServiceName, "*") {
		return nil, fmt.Errorf("wildcard * as prefix or suffix yet supported in full service name")
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

// addServiceToWorkloadLabelMapping maps the namespace and service name to workload labels that its rules originally
// applies to.
func (ug *Upgrader) addServiceToWorkloadLabelMapping(namespace, serviceName string) error {
	if _, found := ug.NamespaceToServiceToWorkloadLabels[namespace][serviceName]; found {
		return nil
	}

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

// parseConfigToString parses data from `config` to string.
func (ug *Upgrader) parseConfigToString(config model.Config) error {
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
			ug.ConvertedPolicies.WriteString("---\n")
		} else if !strings.Contains(configLine, "creationTimestamp: null") {
			ug.ConvertedPolicies.WriteString(fmt.Sprintf("%s\n", configLine))
		}
	}
	return nil
}

// getIstioRootNamespace returns the root namespace of an Istio mesh. The root namespace is located
// at the MeshConfig, and can be provided from the users via a file or directly from kubectl.
func (ug *Upgrader) getIstioRootNamespace() (string, error) {
	var meshConfig *v1alpha1.MeshConfig
	var err error
	if ug.MeshConfigFile != "" {
		if meshConfig, err = cmd.ReadMeshConfig(ug.MeshConfigFile); err != nil {
			return "", fmt.Errorf("failed to read the provided ConfigMap %s: %s", ug.MeshConfigFile, err)
		}
	} else {
		istioConfigMap, err := ug.K8sClient.CoreV1().ConfigMaps(ug.IstioNamespace).Get(ug.MeshConfigMapName, metav1.GetOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to get ConfigMap named %s in namespace %s. "+
				"Run `kubectl -n %s get configmap %s` to see if it exists", ug.MeshConfigMapName, ug.IstioNamespace,
				ug.IstioNamespace, ug.MeshConfigMapName)
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
