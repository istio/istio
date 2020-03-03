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

package model

import (
	"fmt"

	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"
	"github.com/hashicorp/go-multierror"

	istio_rbac "istio.io/api/rbac/v1alpha1"
	security "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	istiolog "istio.io/pkg/log"
)

const (
	// RBACHTTPFilterName is the name of the RBAC http filter in envoy.
	RBACHTTPFilterName = "envoy.filters.http.rbac"

	// RBACTCPFilterName is the name of the RBAC network filter in envoy.
	RBACTCPFilterName       = "envoy.filters.network.rbac"
	RBACTCPFilterStatPrefix = "tcp."

	// attributes that could be used in both ServiceRoleBinding and ServiceRole.
	attrRequestHeader = "request.headers" // header name is surrounded by brackets, e.g. "request.headers[User-Agent]".

	// attributes that could be used in a ServiceRoleBinding property.
	attrSrcIP        = "source.ip"        // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrSrcNamespace = "source.namespace" // e.g. "default".
	// TODO(pitlv2109): Since attrSrcUser will be deprecated, maybe remove this and use attrSrcPrincipal consistently everywhere?
	attrSrcUser            = "source.user"                 // source identity, e.g. "cluster.local/ns/default/sa/productpage".
	attrSrcPrincipal       = "source.principal"            // source identity, e,g, "cluster.local/ns/default/sa/productpage".
	attrRequestPrincipal   = "request.auth.principal"      // authenticated principal of the request.
	attrRequestAudiences   = "request.auth.audiences"      // intended audience(s) for this authentication information.
	attrRequestPresenter   = "request.auth.presenter"      // authorized presenter of the credential.
	attrRequestClaims      = "request.auth.claims"         // claim name is surrounded by brackets, e.g. "request.auth.claims[iss]".
	attrRequestClaimGroups = "request.auth.claims[groups]" // groups claim.

	// reserved string values in names and not_names in ServiceRoleBinding.
	// This prevents ambiguity when the user defines "*" for names or not_names.
	allUsers              = "allUsers"              // Allow all users, both authenticated and unauthenticated.
	allAuthenticatedUsers = "allAuthenticatedUsers" // Allow all authenticated users.

	// attributes that could be used in a ServiceRole constraint.
	attrDestIP        = "destination.ip"        // supports both single ip and cidr, e.g. "10.1.2.3" or "10.1.0.0/16".
	attrDestPort      = "destination.port"      // must be in the range [0, 65535].
	attrDestLabel     = "destination.labels"    // label name is surrounded by brackets, e.g. "destination.labels[version]".
	attrDestName      = "destination.name"      // short service name, e.g. "productpage".
	attrDestNamespace = "destination.namespace" // e.g. "default".
	attrDestUser      = "destination.user"      // service account, e.g. "bookinfo-productpage".
	attrConnSNI       = "connection.sni"        // server name indication, e.g. "www.example.com".

	// Envoy config attributes for ServiceRole rules.
	methodHeader = ":method"
	pathHeader   = ":path"
	pathMatcher  = "path-matcher"
	hostHeader   = ":authority"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

// ServiceMetadata is a collection of different kind of information about a service.
type ServiceMetadata struct {
	Name       string            // full qualified service name, e.g. "productpage.default.svc.cluster.local
	Labels     map[string]string // labels of the service instance
	Attributes map[string]string // additional attributes of the service
}

func NewServiceMetadata(name string, namespace string, service *model.ServiceInstance) (*ServiceMetadata, error) {
	if namespace == "" {
		return nil, fmt.Errorf("found empty namespace")
	}

	return &ServiceMetadata{
		Name:   string(service.Service.Hostname),
		Labels: service.Endpoint.Labels,
		Attributes: map[string]string{
			attrDestName:      name,
			attrDestNamespace: namespace,
			attrDestUser:      extractActualServiceAccount(service.Endpoint.ServiceAccount),
		},
	}, nil
}

func (sm *ServiceMetadata) GetNamespace() string {
	return sm.Attributes[attrDestNamespace]
}

// Model includes a group of permission and principals defining the access control semantics. The
// Permissions specify a list of allowed actions, the Principals specify a list of allowed source
// identities. A request is allowed if it matches any of the permissions and any of the principals.
type Model struct {
	Permissions []Permission
	Principals  []Principal
}

type Values struct {
	Values    []string
	NotValues []string
}

type KeyValues map[string]Values

// NewModelV1alpha1 constructs a Model from a single ServiceRole and a list of ServiceRoleBinding. The ServiceRole
// is converted to the permission and the ServiceRoleBinding is converted to the principal.
func NewModelV1alpha1(trustDomainBundle trustdomain.Bundle, role *istio_rbac.ServiceRole, bindings []*istio_rbac.ServiceRoleBinding) *Model {
	m := &Model{}
	for _, accessRule := range role.Rules {
		var permission Permission
		permission.Services = accessRule.Services
		permission.Hosts = accessRule.Hosts
		permission.NotHosts = accessRule.NotHosts
		permission.Paths = accessRule.Paths
		permission.NotPaths = accessRule.NotPaths
		permission.Methods = accessRule.Methods
		permission.NotMethods = accessRule.NotMethods
		permission.Ports = convertPortsToString(accessRule.Ports)
		permission.NotPorts = convertPortsToString(accessRule.NotPorts)

		constraints := KeyValues{}
		for _, constraint := range accessRule.Constraints {
			constraints[constraint.Key] = Values{Values: constraint.Values}
		}
		permission.Constraints = []KeyValues{constraints}

		m.Permissions = append(m.Permissions, permission)
	}

	for _, binding := range bindings {
		for _, subject := range binding.Subjects {
			users := []string{}
			if subject.User != "" {
				users = trustDomainBundle.ReplaceTrustDomainAliases([]string{subject.User})
			}
			principal := Principal{
				Users:         users,
				Names:         subject.Names,
				NotNames:      subject.NotNames,
				Groups:        subject.Groups,
				Group:         subject.Group,
				NotGroups:     subject.NotGroups,
				Namespaces:    subject.Namespaces,
				NotNamespaces: subject.NotNamespaces,
				IPs:           subject.Ips,
				NotIPs:        subject.NotIps,
			}

			property := KeyValues{}
			for k, v := range subject.Properties {
				values := Values{Values: []string{v}}
				if k == attrSrcPrincipal {
					// TODO(pitlv2109): Refactor this by creating a method for Principal
					// that searches and replaces all trust domains in both v1alpha1 and v1beta1.
					values = Values{Values: trustDomainBundle.ReplaceTrustDomainAliases([]string{v})}
				}
				property[k] = values
			}
			principal.Properties = []KeyValues{property}

			m.Principals = append(m.Principals, principal)
		}
	}

	return m
}

// NewModelV1beta1 constructs a Model from v1beta1 Rule.
func NewModelV1beta1(trustDomainBundle trustdomain.Bundle, rule *security.Rule) *Model {
	m := &Model{}

	conditionsForPrincipal := make([]KeyValues, 0)
	conditionsForPermission := make([]KeyValues, 0)
	for _, when := range rule.When {
		if isSupportedPrincipal(when.Key) {
			values := when.Values
			notValues := when.NotValues
			if when.Key == attrSrcPrincipal {
				if len(values) > 0 {
					values = trustDomainBundle.ReplaceTrustDomainAliases(when.Values)
				}
				if len(notValues) > 0 {
					notValues = trustDomainBundle.ReplaceTrustDomainAliases(when.NotValues)
				}
			}
			conditionsForPrincipal = append(conditionsForPrincipal, KeyValues{
				when.Key: Values{
					Values:    values,
					NotValues: notValues,
				},
			})
		} else if isSupportedPermission(when.Key) {
			conditionsForPermission = append(conditionsForPermission, KeyValues{
				when.Key: Values{
					Values:    when.Values,
					NotValues: when.NotValues,
				},
			})
		} else {
			rbacLog.Errorf("ignored unsupported condition: %v", when)
		}
	}

	for _, from := range rule.From {
		if source := from.Source; source != nil {
			names := source.Principals
			if len(names) > 0 {
				names = trustDomainBundle.ReplaceTrustDomainAliases(names)
			}
			notNames := source.NotPrincipals
			if len(notNames) > 0 {
				notNames = trustDomainBundle.ReplaceTrustDomainAliases(notNames)
			}
			principal := Principal{
				IPs:                  source.IpBlocks,
				NotIPs:               source.NotIpBlocks,
				Names:                names,
				NotNames:             notNames,
				Namespaces:           source.Namespaces,
				NotNamespaces:        source.NotNamespaces,
				RequestPrincipals:    source.RequestPrincipals,
				NotRequestPrincipals: source.NotRequestPrincipals,
				Properties:           conditionsForPrincipal,
				v1beta1:              true,
			}
			m.Principals = append(m.Principals, principal)
		}
	}
	if len(rule.From) == 0 {
		if len(conditionsForPrincipal) != 0 {
			m.Principals = []Principal{{
				Properties: conditionsForPrincipal,
				v1beta1:    true,
			}}
		} else {
			m.Principals = []Principal{{
				AllowAll: true,
				v1beta1:  true,
			}}
		}
	}

	for _, to := range rule.To {
		if operation := to.Operation; operation != nil {
			permission := Permission{
				Methods:     operation.Methods,
				NotMethods:  operation.NotMethods,
				Hosts:       operation.Hosts,
				NotHosts:    operation.NotHosts,
				Ports:       operation.Ports,
				NotPorts:    operation.NotPorts,
				Paths:       operation.Paths,
				NotPaths:    operation.NotPaths,
				Constraints: conditionsForPermission,
				v1beta1:     true,
			}
			m.Permissions = append(m.Permissions, permission)
		}
	}
	if len(rule.To) == 0 {
		if len(conditionsForPermission) != 0 {
			m.Permissions = []Permission{{
				Constraints: conditionsForPermission,
				v1beta1:     true,
			}}
		} else {
			m.Permissions = []Permission{{
				AllowAll: true,
				v1beta1:  true,
			}}
		}
	}

	return m
}

// Generate generates the envoy RBAC filter policy based on the permission and principals specified
// in the model for the given service. This function only generates the policy if the constraints
// and properties specified in the model is matched with the given service. It also validates if the
// model is valid for TCP filter.
// When the policy uses HTTP fields for TCP filter (forTCPFilter is true):
// - If it's allow policy (forDenyPolicy is false), returns nil so that the allow policy is ignored to avoid granting more permissions in this case.
// - If it's deny policy (forDenyPolicy is true), returns a config that only includes the TCP fields (e.g. port) from the policy. This makes sure
//   the generated deny policy is more restrictive so that it never grants extra permission in this case.
func (m *Model) Generate(service *ServiceMetadata, forTCPFilter, forDenyPolicy bool) *envoy_rbac.Policy {
	policy := &envoy_rbac.Policy{}
	for _, permission := range m.Permissions {
		if service == nil || permission.Match(service) {
			p, err := permission.Generate(forTCPFilter, forDenyPolicy)
			if err != nil {
				rbacLog.Debugf("ignored HTTP permission for TCP service: %v", err)
				continue
			}

			policy.Permissions = append(policy.Permissions, p)
		}
	}
	if len(policy.Permissions) == 0 {
		rbacLog.Debugf("role skipped for no permission matched")
		return nil
	}

	for _, principal := range m.Principals {
		p, err := principal.Generate(forTCPFilter, forDenyPolicy)
		if err != nil {
			rbacLog.Debugf("ignored HTTP principal for TCP service: %v", err)
			continue
		}

		policy.Principals = append(policy.Principals, p)
	}
	if len(policy.Principals) == 0 {
		rbacLog.Debugf("role skipped for no principals found")
		return nil
	}
	return policy
}

// ValidateForTCPFilter validates that the model is valid for building a RBAC TCP filter.
func (m *Model) ValidateForTCPFilter() error {
	var errs *multierror.Error
	for _, permission := range m.Permissions {
		if err := permission.ValidateForTCP(true); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	for _, principal := range m.Principals {
		if err := principal.ValidateForTCP(true); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	return errs.ErrorOrNil()
}
