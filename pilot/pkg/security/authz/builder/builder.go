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
	http_filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	tcp_config "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/rbac/v2"

	istiolog "istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/security/authz/policy"
	"istio.io/istio/pilot/pkg/security/authz/policy/v1alpha1"
	"istio.io/istio/pilot/pkg/security/authz/policy/v1beta1"
	"istio.io/istio/pilot/pkg/security/trustdomain"
	"istio.io/istio/pkg/config/labels"
)

var (
	rbacLog = istiolog.RegisterScope("rbac", "rbac debugging", 0)
)

// Builder wraps all needed information for building the RBAC filter for a service.
type Builder struct {
	isXDSMarshalingToAnyEnabled bool
	generator                   policy.Generator
}

// NewBuilder creates a builder instance that can be used to build corresponding RBAC filter config.
func NewBuilder(trustDomainBundle trustdomain.Bundle, serviceInstance *model.ServiceInstance,
	workloadLabels labels.Collection, configNamespace string,
	policies *model.AuthorizationPolicies, isXDSMarshalingToAnyEnabled bool) *Builder {
	var generator policy.Generator

	if p := policies.ListAuthorizationPolicies(configNamespace, workloadLabels); len(p) > 0 {
		generator = v1beta1.NewGenerator(trustDomainBundle, p)
		rbacLog.Debugf("v1beta1 authorization enabled for workload %v in %s", workloadLabels, configNamespace)
	} else {
		if serviceInstance == nil {
			return nil
		}
		if serviceInstance.Service == nil {
			rbacLog.Errorf("no service for serviceInstance: %v", serviceInstance)
			return nil
		}
		serviceName := serviceInstance.Service.Attributes.Name
		serviceNamespace := serviceInstance.Service.Attributes.Namespace
		serviceMetadata, err := authz_model.NewServiceMetadata(serviceName, serviceNamespace, serviceInstance)
		if err != nil {
			rbacLog.Errorf("failed to create ServiceMetadata for %s: %s", serviceName, err)
			return nil
		}

		serviceHostname := string(serviceInstance.Service.Hostname)
		if policies.IsRBACEnabled(serviceHostname, serviceNamespace) {
			generator = v1alpha1.NewGenerator(trustDomainBundle, serviceMetadata, policies, policies.IsGlobalPermissiveEnabled())
			rbacLog.Debugf("v1alpha1 RBAC enabled for service %s", serviceHostname)
		}
	}

	if generator == nil {
		return nil
	}

	return &Builder{
		isXDSMarshalingToAnyEnabled: isXDSMarshalingToAnyEnabled,
		generator:                   generator,
	}
}

// BuildHTTPFilter builds the RBAC HTTP filter.
func (b *Builder) BuildHTTPFilter() *http_filter.HttpFilter {
	if b == nil {
		return nil
	}

	rbacConfig := b.generator.Generate(false /* forTCPFilter */)
	if rbacConfig == nil {
		return nil
	}
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

// BuildTCPFilter builds the RBAC TCP filter.
func (b *Builder) BuildTCPFilter() *tcp_filter.Filter {
	if b == nil {
		return nil
	}

	// The build function always return the config for HTTP filter, we need to extract the
	// generated rules and set it in the config for TCP filter.
	config := b.generator.Generate(true /* forTCPFilter */)
	if config == nil {
		return nil
	}
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
