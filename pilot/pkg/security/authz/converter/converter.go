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
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"text/template"

	"github.com/gogo/protobuf/proto"

	rbac_v1alpha1 "istio.io/api/rbac/v1alpha1"
	rbac_v1beta1 "istio.io/api/security/v1beta1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/log"
)

var (
	allowAllPolicy = &rbac_v1beta1.AuthorizationPolicy{
		Rules: []*rbac_v1beta1.Rule{
			{},
		},
	}
	emptySource    = &rbac_v1beta1.Source{}
	emptyOperation = &rbac_v1beta1.Operation{}
)

// WorkloadLabels is the workload labels, for example, app: productpage.
type WorkloadLabels map[string]string

// ServiceToWorkloadLabels maps the short service name to the workload labels that it's pointing to.
// This service is defined in same namespace as the ServiceRole that's using it.
type ServiceToWorkloadLabels map[string]WorkloadLabels

type v1beta1Policy struct {
	model.AuthorizationPolicyConfig
	comment string
}

type converter struct {
	authorizationPolicies        *model.AuthorizationPolicies
	namespaceToServiceToSelector map[string]ServiceToWorkloadLabels
	allowNoClusterRbacConfig     bool
	generatedPolicies            []*v1beta1Policy
}

func (p v1beta1Policy) write(out io.Writer) error {
	v1beta1PolicyTemplate := `# {{ .Comment }}
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
spec:
  {{ .Spec }}
---
`

	v1beta1Template, err := template.New("").Parse(v1beta1PolicyTemplate)
	if err != nil {
		return err
	}

	spec := "{}"
	if p.AuthorizationPolicy != nil {
		if spec, err = protomarshal.ToYAML(p.AuthorizationPolicy); err != nil {
			return fmt.Errorf("failed to marshal generated policy: %v", err)
		}
		specs := strings.Split(spec, "\n")
		spec = strings.Join(specs, "\n  ")
		spec = strings.TrimSpace(spec)
	}

	data := map[string]string{
		"Comment":   p.comment,
		"Name":      p.Name,
		"Namespace": p.Namespace,
		"Spec":      spec,
	}

	if err := v1beta1Template.Execute(out, data); err != nil {
		return fmt.Errorf("failed to write the config to string: %v", err)
	}

	return nil
}

// Convert converts v1alphal1 RBAC policies to v1beta1 authorization policies.
func Convert(authorizationPolicies *model.AuthorizationPolicies, namespaceToServiceToSelector map[string]ServiceToWorkloadLabels,
	allowNoClusterRbacConfig bool) (string, error) {
	converter := converter{
		authorizationPolicies:        authorizationPolicies,
		namespaceToServiceToSelector: namespaceToServiceToSelector,
		allowNoClusterRbacConfig:     allowNoClusterRbacConfig,
	}
	return converter.convert()
}

func (c *converter) addV1beta1Policy(policies ...*v1beta1Policy) {
	c.generatedPolicies = append(c.generatedPolicies, policies...)
}

func (c *converter) convert() (string, error) {
	if err := c.convertClusterRbacConfig(); err != nil {
		return "", err
	}

	namespaces := c.authorizationPolicies.ListV1alpha1Namespaces()
	log.Infof("found v1alpha1 RBAC policies in %d namespaces: %s", len(namespaces), namespaces)
	for _, ns := range namespaces {
		bindingsForRole := c.authorizationPolicies.ListServiceRoleBindings(ns)
		for _, role := range c.authorizationPolicies.ListServiceRoles(ns) {
			if bindings, found := bindingsForRole[role.Name]; found {
				policies, err := c.convertRoleAndBindings(role, bindings, ns)
				if err != nil {
					log.Errorf("failed to convert ServiceRole %q in namespace %s: %v", role.Name, ns, err)
					return "", err
				}
				var names []string
				for _, p := range policies {
					names = append(names, p.Name)
				}
				log.Infof("converted ServiceRole %q in namespace %s to %d authorization policies: %s",
					role.Name, ns, len(policies), names)
				c.addV1beta1Policy(policies...)
			} else {
				log.Warnf("ignored ServiceRole %q in namespace %s for missing ServiceRoleBinding", role.Name, ns)
			}
		}
	}

	var out strings.Builder
	for _, p := range c.generatedPolicies {
		if err := p.write(&out); err != nil {
			return "", fmt.Errorf("failed to write the config to string: %v", err)
		}
	}

	log.Warnf("PLEASE ALWAYS REVIEW THE CONVERTED POLICIES BEFORE APPLYING.")
	return out.String(), nil
}

func (c *converter) convertClusterRbacConfig() error {
	clusterRbacConfig := c.authorizationPolicies.GetClusterRbacConfig()
	if clusterRbacConfig == nil {
		if c.allowNoClusterRbacConfig {
			log.Error("no ClusterRbacConfig found in the cluster, ignored with --allowNoClusterRbacConfig")
			return nil
		}
		log.Error("no ClusterRbacConfig found in the cluster. To ignore this error, pass: --allowNoClusterRbacConfig")
		return fmt.Errorf("no ClusterRbacConfig")
	}

	rootNamespace := c.authorizationPolicies.RootNamespace
	switch clusterRbacConfig.Mode {
	case rbac_v1alpha1.RbacConfig_OFF:
		name := "generated-global-allow-all"
		comment := "Generated from ClusterRbacConfig with mode OFF. This policy is in root namespace and will allow " +
			"all requests in the mesh by default."
		c.addV1beta1Policy(createPolicy(name, rootNamespace, comment, allowAllPolicy))
		log.Infof("converted ClusterRbacConfig with mode OFF to authorization policy %q in root namespace %q",
			name, rootNamespace)
	case rbac_v1alpha1.RbacConfig_ON:
		name := "generated-global-deny-all"
		comment := "Generated from ClusterRbacConfig with mode ON. " +
			"This policy is in root namespace and will deny all requests in the mesh by default."
		c.addV1beta1Policy(createPolicy(name, rootNamespace, comment, nil))
		log.Infof("converted ClusterRbacConfig with mode ON to authorization policy %q in root namespace %q",
			name, rootNamespace)
	case rbac_v1alpha1.RbacConfig_ON_WITH_INCLUSION:
		for _, service := range clusterRbacConfig.Inclusion.GetServices() {
			svc, namespace, selectors, err := c.extractService(service)
			if err != nil {
				return err
			}
			if len(selectors) == 0 {
				log.Warnf("ignored service %q in ClusterRbacConfig: no such service in cluster", service)
				continue
			}
			name := fmt.Sprintf("generated-service-%s-deny-all", svc)
			comment := fmt.Sprintf("Generated from ClusterRbacConfig with mode ON_WITH_INCLUSION on service %q. "+
				"This policy will deny all requests to service %q by default.", service, service)
			denyAll := &rbac_v1beta1.AuthorizationPolicy{
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: selectors[0],
				},
			}
			c.addV1beta1Policy(createPolicy(name, namespace, comment, denyAll))
			log.Infof("converted ClusterRbacConfig with mode ON_WITH_INCLUSION on service %q to "+
				"authorization policy %q in namespace %q",
				service, name, namespace)
		}
		for _, ns := range clusterRbacConfig.Inclusion.GetNamespaces() {
			name := "generated-namespace-deny-all"
			comment := fmt.Sprintf("Generated from ClusterRbacConfig with mode ON_WITH_INCLUSION on namespace %q. "+
				"This policy will deny all requests to namespace %q by default.", ns, ns)
			c.addV1beta1Policy(createPolicy(name, ns, comment, nil))
			log.Infof("converted ClusterRbacConfig with mode ON_WITH_INCLUSION on namespace %q to "+
				"authorization policy %q in namespace %q",
				ns, name, ns)
		}
	case rbac_v1alpha1.RbacConfig_ON_WITH_EXCLUSION:
		for _, service := range clusterRbacConfig.Exclusion.GetServices() {
			svc, namespace, selectors, err := c.extractService(service)
			if err != nil {
				return err
			}
			if len(selectors) == 0 {
				log.Warnf("ignored service %q in ClusterRbacConfig: no such service in cluster", service)
				continue
			}

			name := fmt.Sprintf("generated-service-%s-allow-all", svc)
			comment := fmt.Sprintf("Generated from ClusterRbacConfig with mode ON_WITH_EXCLUSION on service %q. "+
				"This policy will allow all requests to service %q by default.", service, service)
			allowAll := &rbac_v1beta1.AuthorizationPolicy{
				Selector: &v1beta1.WorkloadSelector{
					MatchLabels: selectors[0],
				},
				Rules: []*rbac_v1beta1.Rule{
					{},
				},
			}
			c.addV1beta1Policy(createPolicy(name, namespace, comment, allowAll))
			log.Infof("converted ClusterRbacConfig with mode ON_WITH_EXCLUSION on service %q to "+
				"authorization policy %q in namespace %q",
				service, name, namespace)
			log.Warnf("ClusterRbacConfig with mode ON_WITH_EXCLUSION is used with service. Note that every " +
				"ServiceRole in the cluster will be converted even if it's not specified by the service in ClusterRbacConfig. " +
				"Please review the converted policies and remove the unneeded ones before applying.")
		}

		name := "generated-global-deny-all"
		comment := "Generated from ClusterRbacConfig with mode ON_WITH_EXCLUSION. " +
			"This policy is in root namespace and will deny all requests in the mesh by default."
		c.addV1beta1Policy(createPolicy(name, rootNamespace, comment, nil))
		log.Infof("converted ClusterRbacConfig with mode ON_WITH_EXCLUSION to authorization policy %q in root namespace %q",
			name, rootNamespace)

		for _, ns := range clusterRbacConfig.Exclusion.GetNamespaces() {
			name := "generated-namespace-allow-all"
			comment := fmt.Sprintf("Generated from ClusterRbacConfig with mode ON_WITH_EXCLUSION on namespace %q. "+
				"This policy will allow all requests to namespace %q by default.", ns, ns)
			c.addV1beta1Policy(createPolicy(name, ns, comment, allowAllPolicy))
			log.Infof("converted ClusterRbacConfig with mode ON_WITH_EXCLUSION on namespace %q to "+
				"authorization policy %q in namespace %q",
				ns, name, ns)
		}
	}
	return nil
}

func createPolicy(name, namespace, comment string, policy *rbac_v1beta1.AuthorizationPolicy) *v1beta1Policy {
	return &v1beta1Policy{
		AuthorizationPolicyConfig: model.AuthorizationPolicyConfig{
			Name:                name,
			Namespace:           namespace,
			AuthorizationPolicy: policy,
		},
		comment: comment,
	}
}

func (c *converter) extractService(service string) (string, string, []WorkloadLabels, error) {
	splits := strings.Split(service, ".")
	if len(splits) < 2 {
		return "", "", nil, fmt.Errorf("invalid service in ClusterRbacConfig: %s", service)
	}
	namespace := splits[1]
	selectors, err := c.getSelectors(service, namespace, nil)
	if err != nil {
		return "", "", nil, err
	}
	if len(selectors) > 1 {
		return "", "", nil, fmt.Errorf("wildcard is not supported in ClusterRbacConfig: %s", service)
	}
	return splits[0], namespace, selectors, nil
}

func (c *converter) convertRoleAndBindings(role model.ServiceRoleConfig, bindings []*rbac_v1alpha1.ServiceRoleBinding,
	namespace string) ([]*v1beta1Policy, error) {
	basePolicy, err := createBasePolicy(bindings)
	if err != nil {
		return nil, err
	}

	var ret []*v1beta1Policy
	for i, rule := range role.ServiceRole.Rules {
		copiedBasePolicy := proto.Clone(basePolicy).(*rbac_v1beta1.AuthorizationPolicy)
		generatedPolicy, err := addAccessRuleToBasePolicy(rule, copiedBasePolicy)
		if err != nil {
			return nil, err
		}

		for j, service := range rule.Services {
			selectors, err := c.getSelectors(service, namespace, rule.Constraints)
			if err != nil {
				return nil, err
			}

			if len(selectors) == 0 {
				log.Warnf("ignored service %q in ServiceRole %q: no such service in cluster", role.Name, service)
			}
			for k, selector := range selectors {
				copiedPolicy := proto.Clone(generatedPolicy).(*rbac_v1beta1.AuthorizationPolicy)
				if len(selector) != 0 {
					copiedPolicy.Selector = &v1beta1.WorkloadSelector{
						MatchLabels: selector,
					}
				}
				ret = append(ret, &v1beta1Policy{
					comment: fmt.Sprintf("Generated for service %q found in ServiceRole %q at rule %d", service, role.Name, i),
					AuthorizationPolicyConfig: model.AuthorizationPolicyConfig{
						Name:                fmt.Sprintf("generated-%s-rule%d-svc%d-target%d", role.Name, i, j, k),
						Namespace:           namespace,
						AuthorizationPolicy: copiedPolicy,
					},
				})
			}
		}
		if len(rule.Services) == 0 {
			ret = append(ret, &v1beta1Policy{
				comment: fmt.Sprintf("Generated for ServiceRole %q at rule %d", role.Name, i),
				AuthorizationPolicyConfig: model.AuthorizationPolicyConfig{
					Name:                fmt.Sprintf("generated-%s-rule%d", role.Name, i),
					Namespace:           namespace,
					AuthorizationPolicy: generatedPolicy,
				},
			})
		}
	}
	return ret, nil
}

func createBasePolicy(bindings []*rbac_v1alpha1.ServiceRoleBinding) (*rbac_v1beta1.AuthorizationPolicy, error) {
	policy := &rbac_v1beta1.AuthorizationPolicy{}
	for _, binding := range bindings {
		for _, subject := range binding.Subjects {
			source := &rbac_v1beta1.Source{}
			var when []*rbac_v1beta1.Condition

			if subject.User != "" && subject.User != "*" {
				source.Principals = []string{
					subject.User,
				}
			}

			if subject.Group != "" && subject.Group != "*" {
				when = append(when, &rbac_v1beta1.Condition{
					Key:    "request.auth.claims[group]",
					Values: []string{subject.Group},
				})
			}

			for k, v := range subject.Properties {
				if k == "source.principal" {
					source.Principals = append(source.Principals, v)
				}
				if v == "*" {
					// Ignore wildcard for property other than source.principal.
					log.Warnf("ignored property %q with wildcard value", k)
					continue
				}
				switch k {
				case "source.ip":
					source.IpBlocks = []string{v}
				case "source.namespace":
					source.Namespaces = []string{v}
				case "request.auth.principal":
					source.RequestPrincipals = []string{v}
				default:
					when = append(when, &rbac_v1beta1.Condition{
						Key:    k,
						Values: []string{v},
					})
				}
			}
			rule := &rbac_v1beta1.Rule{
				When: when,
			}
			if !reflect.DeepEqual(emptySource, source) {
				rule.From = []*rbac_v1beta1.Rule_From{
					{
						Source: source,
					},
				}
			}
			policy.Rules = append(policy.Rules, rule)
		}
	}

	if len(policy.Rules) == 0 {
		return nil, fmt.Errorf("basePolicy must has at least 1 rule")
	}
	return policy, nil
}

func addAccessRuleToBasePolicy(rule *rbac_v1alpha1.AccessRule, basePolicy *rbac_v1beta1.AuthorizationPolicy) (
	*rbac_v1beta1.AuthorizationPolicy, error) {
	operation := &rbac_v1beta1.Operation{}
	var when []*rbac_v1beta1.Condition
	for _, path := range rule.Paths {
		if path != "*" {
			operation.Paths = append(operation.Paths, path)
		}
	}
	for _, method := range rule.Methods {
		if method != "*" {
			operation.Methods = append(operation.Methods, method)
		}
	}
	for _, constraint := range rule.Constraints {
		if strings.HasPrefix(constraint.Key, "destination.labels") {
			continue
		}
		switch constraint.Key {
		case "destination.ip", "destination.user":
			return nil, fmt.Errorf("%s is no longer supported in v1beta1 authorization policy", constraint.Key)
		case "destination.port":
			operation.Ports = constraint.Values
		case "destination.namespace":
		default:
			var values []string
			for _, v := range constraint.Values {
				if v == "*" {
					log.Warnf("ignored constraint %q with wildcard value", constraint.Key)
					continue
				}
				values = append(values, v)
			}
			if len(values) > 0 {
				when = append(when, &rbac_v1beta1.Condition{
					Key:    constraint.Key,
					Values: values,
				})
			}
		}
	}

	for _, r := range basePolicy.Rules {
		if !reflect.DeepEqual(operation, emptyOperation) {
			copiedOperation := proto.Clone(operation).(*rbac_v1beta1.Operation)
			r.To = []*rbac_v1beta1.Rule_To{
				{
					Operation: copiedOperation,
				},
			}
		}
		r.When = append(r.When, when...)

		// Sort the conditions to generate a stable output.
		sort.SliceStable(r.When, func(i, j int) bool {
			return r.When[i].Key < r.When[j].Key
		})
	}

	return basePolicy, nil
}

func (c *converter) getSelectors(service, namespace string, constraints []*rbac_v1alpha1.AccessRule_Constraint) (
	[]WorkloadLabels, error) {
	selectorsFromService := getSelectorsFromService(service, c.namespaceToServiceToSelector[namespace])
	selectorsFromConstraint, err := getSelectorsFromConstraints(constraints)
	if err != nil {
		return nil, err
	}
	return mergeSelectors(selectorsFromService, selectorsFromConstraint)
}

func getSelectorsFromService(service string, serviceToSelectors ServiceToWorkloadLabels) []WorkloadLabels {
	if service == "*" {
		return []WorkloadLabels{
			// Empty selects all workloads in the namespace.
			{},
		}
	}

	var selectorsFromService []WorkloadLabels
	var trimmedService string
	var prefixMatch, suffixMatch bool
	if strings.HasPrefix(service, "*") {
		suffixMatch = true
		trimmedService = strings.TrimPrefix(service, "*")
	} else if strings.HasSuffix(service, "*") {
		prefixMatch = true
		trimmedService = strings.TrimSuffix(service, "*")
	} else {
		trimmedService = strings.Split(service, ".")[0]
	}

	var targetServices []string
	for svc := range serviceToSelectors {
		targetServices = append(targetServices, svc)
	}
	// Sort the services in the map to make sure the output is stable.
	sort.Strings(targetServices)

	for _, targetService := range targetServices {
		selector := serviceToSelectors[targetService]
		if prefixMatch {
			if strings.HasPrefix(targetService, trimmedService) {
				selectorsFromService = append(selectorsFromService, selector)
			}
		} else if suffixMatch {
			if strings.HasSuffix(targetService, trimmedService) {
				selectorsFromService = append(selectorsFromService, selector)
			}
		} else {
			if targetService == trimmedService {
				selectorsFromService = append(selectorsFromService, selector)
			}
		}
	}
	return selectorsFromService
}

func getSelectorsFromConstraints(constraints []*rbac_v1alpha1.AccessRule_Constraint) ([]WorkloadLabels, error) {
	var selectors [][]WorkloadLabels
	for _, constraint := range constraints {
		if strings.HasPrefix(constraint.Key, "destination.labels[") {
			k := strings.TrimPrefix(constraint.Key, "destination.labels[")
			k = strings.TrimSuffix(k, "]")
			var selector []WorkloadLabels
			for _, v := range constraint.Values {
				selector = append(selector, WorkloadLabels{
					k: v,
				})
			}
			selectors = append(selectors, selector)
		}
	}

	var last []WorkloadLabels
	for _, cur := range selectors {
		merged, err := mergeSelectors(last, cur)
		if err != nil {
			return nil, err
		}
		last = merged
	}

	return last, nil
}

func mergeSelectors(selectors1 []WorkloadLabels, selectors2 []WorkloadLabels) ([]WorkloadLabels, error) {
	if len(selectors1) == 0 {
		return selectors2, nil
	} else if len(selectors2) == 0 {
		return selectors1, nil
	}

	var mergedSelectors []WorkloadLabels
	for _, s1 := range selectors1 {
		for _, s2 := range selectors2 {
			merged, err := mergeWorkloadLabels(s1, s2)
			if err != nil {
				return nil, err
			}
			mergedSelectors = append(mergedSelectors, merged)
		}
	}
	return mergedSelectors, nil
}

func mergeWorkloadLabels(labels1 WorkloadLabels, labels2 WorkloadLabels) (WorkloadLabels, error) {
	mergedLabels := make(WorkloadLabels)
	for k, v := range labels1 {
		mergedLabels[k] = v
	}
	for k, v := range labels2 {
		if previousValue, found := mergedLabels[k]; found && previousValue != v {
			return nil, fmt.Errorf("duplicate workload label %q with conflict values: %q, %q", k, previousValue, v)
		}
		mergedLabels[k] = v
	}
	return mergedLabels, nil
}
