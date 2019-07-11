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

	istio_rbac "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
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
		Labels: service.Labels,
		Attributes: map[string]string{
			attrDestName:      name,
			attrDestNamespace: namespace,
			attrDestUser:      extractActualServiceAccount(service.ServiceAccount),
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

type KeyValues map[string][]string

// NewModel constructs a Model from a single ServiceRole and a list of ServiceRoleBinding. The ServiceRole
// is converted to the permission and the ServiceRoleBinding is converted to the principal.
func NewModel(role *istio_rbac.ServiceRole, bindings []*istio_rbac.ServiceRoleBinding) *Model {
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
		permission.Ports = accessRule.Ports
		permission.NotPorts = accessRule.NotPorts

		constraints := KeyValues{}
		for _, constraint := range accessRule.Constraints {
			constraints[constraint.Key] = constraint.Values
		}
		permission.Constraints = []KeyValues{constraints}

		m.Permissions = append(m.Permissions, permission)
	}

	for _, binding := range bindings {
		for _, subject := range binding.Subjects {
			var principal Principal
			principal.User = subject.User
			principal.Names = subject.Names
			principal.NotNames = subject.NotNames
			principal.Groups = subject.Groups
			principal.Group = subject.Group
			principal.NotGroups = subject.NotGroups
			principal.Namespaces = subject.Namespaces
			principal.NotNamespaces = subject.NotNamespaces
			principal.IPs = subject.Ips
			principal.NotIPs = subject.NotIps

			property := KeyValues{}
			for k, v := range subject.Properties {
				property[k] = []string{v}
			}
			principal.Properties = []KeyValues{property}

			m.Principals = append(m.Principals, principal)
		}
	}

	return m
}

// Generate generates the envoy RBAC filter policy based on the permission and principals specified
// in the model for the given service. This function only generates the policy if the constraints
// and properties specified in the model is matched with the given service. It also validates if the
// model is valid for TCP filter.
func (m *Model) Generate(service *ServiceMetadata, forTCPFilter bool) *envoy_rbac.Policy {
	policy := &envoy_rbac.Policy{}
	for _, permission := range m.Permissions {
		if permission.Match(service) {
			p, err := permission.Generate(forTCPFilter)
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
		p, err := principal.Generate(forTCPFilter)
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
