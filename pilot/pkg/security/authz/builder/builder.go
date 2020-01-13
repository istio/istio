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

	denyPolicies := policies.ListAuthorizationPolicies(configNamespace, workloadLabels, model.DenyPolicy)
	allowPolicies := policies.ListAuthorizationPolicies(configNamespace, workloadLabels, model.AllowPolicy)
	if len(denyPolicies) > 0 || len(allowPolicies) > 0 {
		generator = v1beta1.NewGenerator(trustDomainBundle, allowPolicies, denyPolicies)
		rbacLog.Debugf("found authorization allow policies for workload %v in %s", workloadLabels, configNamespace)
	} else {
		if serviceInstance == nil {
			return nil
		}
		if serviceInstance.Service == nil {
			rbacLog.Errorf("no service for serviceInstance: %v", serviceInstance)
			return nil
		}
		serviceNamespace := serviceInstance.Service.Attributes.Namespace
		serviceHostname := string(serviceInstance.Service.Hostname)
		if policies.IsRBACEnabled(serviceHostname, serviceNamespace) {
			serviceName := serviceInstance.Service.Attributes.Name
			serviceMetadata, err := authz_model.NewServiceMetadata(serviceName, serviceNamespace, serviceInstance)
			if err != nil {
				rbacLog.Errorf("failed to create ServiceMetadata for %s: %s", serviceName, err)
				return nil
			}
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

func createHTTPFilter(config *http_config.RBAC, isXDSMarshalingToAnyEnabled bool) *http_filter.HttpFilter {
	if config == nil {
		return nil
	}

	httpConfig := http_filter.HttpFilter{
		Name: authz_model.RBACHTTPFilterName,
	}
	if isXDSMarshalingToAnyEnabled {
		httpConfig.ConfigType = &http_filter.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(config)}
	} else {
		httpConfig.ConfigType = &http_filter.HttpFilter_Config{Config: util.MessageToStruct(config)}
	}
	return &httpConfig
}

// BuildHTTPFilter builds two RBAC HTTP filter, the first one for deny policies and the second one for allow policies.
func (b *Builder) BuildHTTPFilter() (denyFilter *http_filter.HttpFilter, allowFilter *http_filter.HttpFilter) {
	if b == nil {
		return
	}
	denyConfig, allowConfig := b.generator.Generate(false /* forTCPFilter */)
	denyFilter = createHTTPFilter(denyConfig, b.isXDSMarshalingToAnyEnabled)
	allowFilter = createHTTPFilter(allowConfig, b.isXDSMarshalingToAnyEnabled)
	return
}

// BuildHTTPFilters is a wrapper of BuildHTTPFilter that returns the two filters in a list.
func (b *Builder) BuildHTTPFilters() []*http_filter.HttpFilter {
	var filters []*http_filter.HttpFilter
	denyFilter, allowFilter := b.BuildHTTPFilter()
	if denyFilter != nil {
		filters = append(filters, denyFilter)
	}
	if allowFilter != nil {
		filters = append(filters, allowFilter)
	}
	return filters
}

func createTCPFilter(config *http_config.RBAC, isXDSMarshalingToAnyEnabled bool) *tcp_filter.Filter {
	if config == nil {
		return nil
	}

	// The build function always return the config for HTTP filter, we need to extract the
	// generated rules and set it in the config for TCP filter.
	rbacConfig := &tcp_config.RBAC{
		Rules:       config.Rules,
		ShadowRules: config.ShadowRules,
		StatPrefix:  authz_model.RBACTCPFilterStatPrefix,
	}

	tcpConfig := tcp_filter.Filter{
		Name: authz_model.RBACTCPFilterName,
	}
	if isXDSMarshalingToAnyEnabled {
		tcpConfig.ConfigType = &tcp_filter.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbacConfig)}
	} else {
		tcpConfig.ConfigType = &tcp_filter.Filter_Config{Config: util.MessageToStruct(rbacConfig)}
	}
	return &tcpConfig
}

// BuildTCPFilter builds two RBAC TCP filters, the first one for deny policies and the second one for allow policies.
func (b *Builder) BuildTCPFilter() (denyFilter *tcp_filter.Filter, allowFilter *tcp_filter.Filter) {
	if b == nil {
		return
	}
	denyConfig, allowConfig := b.generator.Generate(true /* forTCPFilter */)
	denyFilter = createTCPFilter(denyConfig, b.isXDSMarshalingToAnyEnabled)
	allowFilter = createTCPFilter(allowConfig, b.isXDSMarshalingToAnyEnabled)
	return
}

// BuildTCPFilters is a wrapper of BuildTCPFilter that returns the two filters in a list.
func (b *Builder) BuildTCPFilters() []*tcp_filter.Filter {
	var filters []*tcp_filter.Filter
	denyFilter, allowFilter := b.BuildTCPFilter()
	if denyFilter != nil {
		filters = append(filters, denyFilter)
	}
	if allowFilter != nil {
		filters = append(filters, allowFilter)
	}
	return filters
}
