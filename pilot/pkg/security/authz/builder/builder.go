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
	tcpFilterPb "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	envoyRbacHttpPb "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	httpFilterPb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	envoyRbacTcpPb "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/rbac/v2"

	istiolog "istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authzModel "istio.io/istio/pilot/pkg/security/authz/model"
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
	generator policy.Generator
}

// NewBuilder creates a builder instance that can be used to build corresponding RBAC filter config.
func NewBuilder(trustDomainBundle trustdomain.Bundle, serviceInstance *model.ServiceInstance,
	workloadLabels labels.Collection, configNamespace string, policies *model.AuthorizationPolicies) *Builder {
	var generator policy.Generator

	denyPolicies, allowPolicies := policies.ListAuthorizationPolicies(configNamespace, workloadLabels)
	if len(denyPolicies) > 0 || len(allowPolicies) > 0 {
		generator = v1beta1.NewGenerator(trustDomainBundle, denyPolicies, allowPolicies)
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
			serviceMetadata, err := authzModel.NewServiceMetadata(serviceName, serviceNamespace, serviceInstance)
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
		generator: generator,
	}
}

// BuildHTTPFilters build RBAC HTTP filters.
func (b *Builder) BuildHTTPFilters() []*httpFilterPb.HttpFilter {
	if b == nil {
		return nil
	}

	var filters []*httpFilterPb.HttpFilter
	denyConfig, allowConfig := b.generator.Generate(false /* forTCPFilter */)
	if denyFilter := createHTTPFilter(denyConfig); denyFilter != nil {
		filters = append(filters, denyFilter)
	}
	if allowFilter := createHTTPFilter(allowConfig); allowFilter != nil {
		filters = append(filters, allowFilter)
	}
	return filters
}

// nolint: interfacer
func createHTTPFilter(config *envoyRbacHttpPb.RBAC) *httpFilterPb.HttpFilter {
	if config == nil {
		return nil
	}

	httpConfig := httpFilterPb.HttpFilter{
		Name:       authzModel.RBACHTTPFilterName,
		ConfigType: &httpFilterPb.HttpFilter_TypedConfig{TypedConfig: util.MessageToAny(config)},
	}
	return &httpConfig
}

// BuildTCPFilters build RBAC TCP filters.
func (b *Builder) BuildTCPFilters() []*tcpFilterPb.Filter {
	if b == nil {
		return nil
	}

	var filters []*tcpFilterPb.Filter
	denyConfig, allowConfig := b.generator.Generate(true /* forTCPFilter */)
	if denyFilter := createTCPFilter(denyConfig); denyFilter != nil {
		filters = append(filters, denyFilter)
	}
	if allowFilter := createTCPFilter(allowConfig); allowFilter != nil {
		filters = append(filters, allowFilter)
	}
	return filters
}

func createTCPFilter(config *envoyRbacHttpPb.RBAC) *tcpFilterPb.Filter {
	if config == nil {
		return nil
	}

	// The build function always return the config for HTTP filter, we need to extract the
	// generated rules and set it in the config for TCP filter.
	rbacConfig := &envoyRbacTcpPb.RBAC{
		Rules:       config.Rules,
		ShadowRules: config.ShadowRules,
		StatPrefix:  authzModel.RBACTCPFilterStatPrefix,
	}

	tcpConfig := tcpFilterPb.Filter{
		Name:       authzModel.RBACTCPFilterName,
		ConfigType: &tcpFilterPb.Filter_TypedConfig{TypedConfig: util.MessageToAny(rbacConfig)},
	}
	return &tcpConfig
}
