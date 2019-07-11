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

package builder

import (
	tcp_filter "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	http_filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	tcp_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/rbac/v2"
	envoy_rbac "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2"

	istio_rbac "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	istiolog "istio.io/pkg/log"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

type Builder struct {
	serviceMetadata             *authz_model.ServiceMetadata
	authzPolicies               *model.AuthorizationPolicies
	isGlobalPermissiveEnabled   bool // True if global RBAC config is in permissive mode.
	isXDSMarshalingToAnyEnabled bool
}

func NewBuilder(serviceInstance *model.ServiceInstance, policies *model.AuthorizationPolicies, isXDSMarshalingToAnyEnabled bool) *Builder {
	if serviceInstance.Service == nil {
		rbacLog.Errorf("no service for serviceInstance: %v", serviceInstance)
		return nil
	}

	serviceName := serviceInstance.Service.Attributes.Name
	serviceNamespace := serviceInstance.Service.Attributes.Namespace
	serviceHostname := string(serviceInstance.Service.Hostname)
	if !isRbacEnabled(serviceHostname, serviceNamespace, policies) {
		rbacLog.Debugf("RBAC disabled for service %s", serviceHostname)
		return nil
	}

	serviceMetadata, err := authz_model.NewServiceMetadata(serviceName, serviceNamespace, serviceInstance)
	if err != nil {
		rbacLog.Errorf("failed to create ServiceMetadata for %s: %s", serviceName, err)
		return nil
	}

	rbacConfig := policies.RbacConfig
	isGlobalPermissiveEnabled := rbacConfig != nil && rbacConfig.EnforcementMode == istio_rbac.EnforcementMode_PERMISSIVE

	return &Builder{
		serviceMetadata:             serviceMetadata,
		authzPolicies:               policies,
		isGlobalPermissiveEnabled:   isGlobalPermissiveEnabled,
		isXDSMarshalingToAnyEnabled: isXDSMarshalingToAnyEnabled,
	}
}

func (b *Builder) BuildHTTPFilter() *http_filter.HttpFilter {
	rbacConfig := b.build(false /* forTCPFilter */)
	httpConfig := http_filter.HttpFilter{
		Name: authz_model.RBACHTTPFilterName,
	}
	if b.isXDSMarshalingToAnyEnabled {
		httpConfig.ConfigType = &http_filter.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(rbacConfig)}
	} else {
		httpConfig.ConfigType = &http_filter.HttpFilter_Config{Config: util.MessageToStruct(rbacConfig)}
	}

	rbacLog.Debugf("built http filter config: %v", httpConfig)
	return &httpConfig
}

func (b *Builder) BuildTCPFilter() *tcp_filter.Filter {
	// The build function always return the config for HTTP filter, we need to extract the
	// generated rules and set it in the config for TCP filter.
	config := b.build(true /* forTCPFilter */)
	rbacConfig := &tcp_config.RBAC{
		Rules:       config.Rules,
		ShadowRules: config.ShadowRules,
		StatPrefix:  authz_model.RBACTCPFilterStatPrefix,
	}

	tcpConfig := tcp_filter.Filter{
		Name: authz_model.RBACTCPFilterName,
	}
	if b.isXDSMarshalingToAnyEnabled {
		tcpConfig.ConfigType = &tcp_filter.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbacConfig)}
	} else {
		tcpConfig.ConfigType = &tcp_filter.Filter_Config{Config: util.MessageToStruct(rbacConfig)}
	}

	rbacLog.Debugf("built tcp filter config: %v", tcpConfig)
	return &tcpConfig
}

// isServiceInList checks if a given service or namespace is found in the RbacConfig target.
func isServiceInList(serviceHostname string, namespace string, target *istio_rbac.RbacConfig_Target) bool {
	if target == nil {
		return false
	}
	for _, ns := range target.Namespaces {
		if namespace == ns {
			return true
		}
	}
	for _, service := range target.Services {
		if service == serviceHostname {
			return true
		}
	}
	return false
}

func isRbacEnabled(serviceHostname string, namespace string, policies *model.AuthorizationPolicies) bool {
	if policies == nil || policies.RbacConfig == nil {
		return false
	}

	rbacConfig := policies.RbacConfig
	switch rbacConfig.Mode {
	case istio_rbac.RbacConfig_ON:
		return true
	case istio_rbac.RbacConfig_ON_WITH_INCLUSION:
		return isServiceInList(serviceHostname, namespace, rbacConfig.Inclusion)
	case istio_rbac.RbacConfig_ON_WITH_EXCLUSION:
		return !isServiceInList(serviceHostname, namespace, rbacConfig.Exclusion)
	default:
		return false
	}
}

func (b *Builder) build(forTCPFiler bool) *http_config.RBAC {
	if b.authzPolicies.IsRbacV2 {
		return b.buildV2(forTCPFiler)
	}
	return b.buildV1(forTCPFiler)
}

func (b *Builder) generate(role *istio_rbac.ServiceRole, bindings []*istio_rbac.ServiceRoleBinding, forTCPFilter bool) *envoy_rbac.Policy {
	if role == nil || len(bindings) == 0 {
		return nil
	}

	m := authz_model.NewModel(role, bindings)
	return m.Generate(b.serviceMetadata, forTCPFilter)
}
