package mseingress

import (
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rbachttppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/ptypes/any"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
)

const (
	extAuthzMatchPrefix = "istio-ext-authz"

	IPAccessControl = "ip-access-control"

	LocalRateLimit = "local-rate-limit"
)

type GlobalHTTPFilters struct {
	rbac *rbachttppb.RBAC
}

func (g *GlobalHTTPFilters) isRBACEmpty() bool {
	if g == nil || g.rbac == nil {
		return true
	}

	if g.rbac.Rules != nil && len(g.rbac.Rules.Policies) != 0 {
		return false
	}

	if g.rbac.ShadowRules != nil && len(g.rbac.ShadowRules.Policies) != 0 {
		return false
	}

	return true
}

func ExtractGlobalHTTPFilters(node *model.Proxy, pushContext *model.PushContext) *GlobalHTTPFilters {
	return &GlobalHTTPFilters{
		rbac: generateRBACFilters(node, pushContext),
	}
}

func ConstructTypedPerFilterConfigForVHost(globalHTTPFilters *GlobalHTTPFilters, virtualService config.Config) map[string]*any.Any {
	vs := virtualService.Spec.(*networking.VirtualService)
	// If host has no explicitly http filter, skip to build and use the parent filters.
	if len(vs.HostHTTPFilters) == 0 {
		return nil
	}

	return doBuildTypedPerFilterConfig(globalHTTPFilters, vs.HostHTTPFilters)
}

func ConstructTypedPerFilterConfigForRoute(globalHTTPFilters *GlobalHTTPFilters, _ config.Config, route *networking.HTTPRoute) map[string]*any.Any {
	// If route has no explicitly http filter, skip to build and use the parent filters.
	if len(route.RouteHTTPFilters) == 0 {
		return nil
	}

	return doBuildTypedPerFilterConfig(globalHTTPFilters, route.RouteHTTPFilters)
}

func doBuildTypedPerFilterConfig(globalHTTPFilters *GlobalHTTPFilters, configFilters []*networking.HTTPFilter) map[string]*any.Any {
	perFilterConfig := make(map[string]*any.Any)

	var rbacFilters []*networking.HTTPFilter
	for _, httpFilter := range configFilters {
		switch httpFilter.Filter.(type) {
		case *networking.HTTPFilter_IpAccessControl:
			rbacFilters = append(rbacFilters, httpFilter)
		case *networking.HTTPFilter_LocalRateLimit:
			AddLocalRateLimitFilter(perFilterConfig, httpFilter)
		}
	}

	if len(rbacFilters) != 0 {
		AddRBACFilter(globalHTTPFilters, perFilterConfig, rbacFilters)
	}

	return perFilterConfig
}

func AddRBACFilter(globalHTTPFilters *GlobalHTTPFilters, perFilterConfig map[string]*any.Any, rbacFilters []*networking.HTTPFilter) {
	perRoute := &rbachttppb.RBACPerRoute{
		Rbac: &rbachttppb.RBAC{
			Rules: &rbacpb.RBAC{
				Action:   rbacpb.RBAC_DENY,
				Policies: make(map[string]*rbacpb.Policy),
			},
		},
	}

	policies := perRoute.Rbac.Rules.Policies
	for _, filter := range rbacFilters {
		if filter.Disable {
			policies[filter.Name] = rbacPolicyMatchNever
			continue
		}

		var policy *rbacpb.Policy
		switch f := filter.Filter.(type) {
		case *networking.HTTPFilter_IpAccessControl:
			policy = buildIpAccessControlPolicy(f.IpAccessControl)
		}

		if policy != nil {
			policies[filter.Name] = policy
		}
	}

	// There are no rbac filters existing, so just ignore it.
	if len(policies) == 0 {
		return
	}

	// If the global has effective rbac filter, we need to inherit rbac policies that host does not have.
	if !globalHTTPFilters.isRBACEmpty() {
		// Now, we directly inherit shadow policies.
		// In the future for oidc or ext authz, we should reconsider there logic.
		perRoute.Rbac.ShadowRules = globalHTTPFilters.rbac.ShadowRules
		if globalHTTPFilters.rbac.Rules != nil {
			for key, policy := range globalHTTPFilters.rbac.Rules.Policies {
				if _, exist := perRoute.Rbac.Rules.Policies[key]; !exist {
					perRoute.Rbac.Rules.Policies[key] = policy
				}
			}
		}
	}

	perFilterConfig[wellknown.HTTPRoleBasedAccessControl] = protoconv.MessageToAny(perRoute)
}
