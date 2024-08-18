package mseingress

import (
	"fmt"
	"strings"

	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rbachttppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"

	networking "istio.io/api/networking/v1alpha3"
	authpb "istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	authzmodel "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pkg/log"
)

const (
	wirecard = "*"

	RBACTypeUrl = "type.googleapis.com/envoy.extensions.filters.http.rbac.v3.RBAC"
)

var (
	// RBACPolicyNameMapping records that the ops rbac policies name mapping
	// to http filter well-known name. E.g. ip-access-control
	RBACPolicyNameMapping = map[string]string{
		"black": IPAccessControl,
	}

	rbacPolicyMatchNever = &rbacpb.Policy{
		Permissions: []*rbacpb.Permission{{Rule: &rbacpb.Permission_NotRule{
			NotRule: &rbacpb.Permission{Rule: &rbacpb.Permission_Any{Any: true}},
		}}},
		Principals: []*rbacpb.Principal{{Identifier: &rbacpb.Principal_NotId{
			NotId: &rbacpb.Principal{Identifier: &rbacpb.Principal_Any{Any: true}},
		}}},
	}
)

func convertHTTPFilterName(name string) string {
	if value, exist := RBACPolicyNameMapping[name]; exist {
		return value
	}

	return name
}

func permissionAny() *rbacpb.Permission {
	return &rbacpb.Permission{
		Rule: &rbacpb.Permission_Any{
			Any: true,
		},
	}
}

func principalOr(principals []*rbacpb.Principal) *rbacpb.Principal {
	return &rbacpb.Principal{
		Identifier: &rbacpb.Principal_OrIds{
			OrIds: &rbacpb.Principal_Set{
				Ids: principals,
			},
		},
	}
}

func principalNot(principal *rbacpb.Principal) *rbacpb.Principal {
	return &rbacpb.Principal{
		Identifier: &rbacpb.Principal_NotId{
			NotId: principal,
		},
	}
}

func buildIpAccessControlPolicy(ipAccessControl *networking.IPAccessControl) *rbacpb.Policy {
	var principals []*rbacpb.Principal
	ipList := ipAccessControl.RemoteIpBlocks
	isWhite := true
	if len(ipAccessControl.NotRemoteIpBlocks) != 0 {
		isWhite = false
		ipList = ipAccessControl.NotRemoteIpBlocks
	}

	for _, ip := range ipList {
		if cidr, err := util.AddrStrToCidrRange(ip); err == nil {
			principals = append(principals, &rbacpb.Principal{
				Identifier: &rbacpb.Principal_RemoteIp{
					RemoteIp: cidr,
				},
			})
		}
	}

	if len(principals) == 0 {
		return nil
	}

	policy := &rbacpb.Policy{
		Permissions: []*rbacpb.Permission{permissionAny()},
	}

	if isWhite {
		policy.Principals = []*rbacpb.Principal{principalNot(principalOr(principals))}
	} else {
		policy.Principals = []*rbacpb.Principal{principalOr(principals)}
	}

	return policy
}

func GetRBACFilter(filter *http_conn.HttpFilter) *rbachttppb.RBAC {
	rbac := &rbachttppb.RBAC{}
	switch c := filter.ConfigType.(type) {
	case *http_conn.HttpFilter_TypedConfig:
		if err := c.TypedConfig.UnmarshalTo(rbac); err != nil {
			log.Debugf("failed to get rbac config: %s", err)
			return nil
		}
	case *http_conn.HttpFilter_ConfigDiscovery:
		return nil
	}
	return rbac
}

func HasRBACFilter(filter *http_conn.HttpFilter) bool {
	switch c := filter.ConfigType.(type) {
	case *http_conn.HttpFilter_TypedConfig:
		rbac := &rbachttppb.RBAC{}
		if err := c.TypedConfig.UnmarshalTo(rbac); err != nil {
			log.Debugf("failed to get rbac config: %s", err)
			return false
		}
		return true
	case *http_conn.HttpFilter_ConfigDiscovery:
		for _, url := range c.ConfigDiscovery.GetTypeUrls() {
			if url == RBACTypeUrl {
				return true
			}
		}
	}

	return false
}

func policyName(name string, extAuthz bool) string {
	prefix := ""
	if extAuthz {
		prefix = extAuthzMatchPrefix + "-"
	}
	parts := strings.Split(name, "-")
	// The authorization policy format name is gw-xxx-istio-key.
	// We previously use key to distinguish different user experience.
	// Here, we convert these keys to well-known http filter name.
	if len(parts) >= 4 {
		name = convertHTTPFilterName(parts[3])
	}
	return fmt.Sprintf("%s%s", prefix, name)
}

func doBuildRBAC(policies []model.AuthorizationPolicy) *rbacpb.RBAC {
	rbac := &rbacpb.RBAC{
		Action:   rbacpb.RBAC_DENY,
		Policies: map[string]*rbacpb.Policy{},
	}

	for _, policy := range policies {
		for _, rule := range policy.Spec.Rules {
			name := policyName(policy.Name, false)
			m, _ := authzmodel.New(rule)
			generated, err := m.Generate(false, false, rbacpb.RBAC_DENY)
			if err != nil {
				continue
			}
			if generated != nil {
				rbac.Policies[name] = generated
			}
		}
		if len(policy.Spec.Rules) == 0 {
			// Generate an explicit policy that never matches.
			name := policyName(policy.Name, false)
			rbac.Policies[name] = rbacPolicyMatchNever
		}
	}

	return rbac
}

func generateRBACFilters(node *model.Proxy, pushContext *model.PushContext) *rbachttppb.RBAC {
	// First obtain from authorization policy
	polices := pushContext.AuthzPolicies.ListAuthorizationPolicies(node.ConfigNamespace, node.Metadata.Labels)
	// In rbac route config, we only can set one action, allow or deny.
	// We convert existing allow polices to deny polices.
	for _, policy := range polices.Allow {
		// Allow anything, so just skip.
		if len(policy.Spec.Rules) > 0 && len(policy.Spec.Rules[0].From) == 0 {
			continue
		}

		if isUsingExtensionPath(policy) {
			polices.Deny = append(polices.Deny, processPolicyWithExtensionPath(policy))
		} else {
			polices.Deny = append(polices.Deny, processPolicyWithPath(policy))
		}
	}

	polices.Allow = nil
	rbac := doBuildRBAC(polices.Deny)

	shadowRbac := &rbacpb.RBAC{
		Action:   rbacpb.RBAC_DENY,
		Policies: map[string]*rbacpb.Policy{},
	}

	httpFilters := pushContext.GetHTTPFiltersFromEnvoyFilter(node)
	for _, filter := range httpFilters {
		rbacFilter := GetRBACFilter(filter)
		if rbacFilter != nil && rbacFilter.ShadowRules != nil {
			for key, value := range rbacFilter.ShadowRules.Policies {
				shadowRbac.Policies[key] = value
			}
		}
	}

	extensionConfigs := pushContext.GetExtensionConfigsFromEnvoyFilter(node)
	for _, extension := range extensionConfigs {
		if extension.GetTypedConfig() == nil {
			continue
		}

		rbacFilter := &rbachttppb.RBAC{}
		if err := extension.TypedConfig.UnmarshalTo(rbacFilter); err != nil {
			log.Debugf("[generateRBACFilters] failed to get rbac config: %s", err)
			continue
		}

		if rbacFilter != nil && rbacFilter.ShadowRules != nil {
			for key, value := range rbacFilter.ShadowRules.Policies {
				shadowRbac.Policies[key] = value
			}
		}
	}

	if len(rbac.Policies) != 0 || len(shadowRbac.Policies) != 0 {
		return &rbachttppb.RBAC{
			Rules:       rbac,
			ShadowRules: shadowRbac,
		}
	}

	return nil
}

func processPolicyWithPath(policy model.AuthorizationPolicy) model.AuthorizationPolicy {
	targetPolicy := model.AuthorizationPolicy{
		Name:        policy.Name,
		Namespace:   policy.Namespace,
		Annotations: policy.Annotations,
		Spec:        &authpb.AuthorizationPolicy{},
	}

	if len(policy.Spec.Rules) > 0 {
		principal := policy.Spec.Rules[0].From[0]

		hostToPathsMap := map[string][]string{}
		var wirecardPaths []string
		var totalPaths []string

		if len(policy.Spec.Rules) > 1 {
			hostPathRule := policy.Spec.Rules[1]
			for _, to := range hostPathRule.To {
				for _, host := range to.Operation.Hosts {
					for _, path := range to.Operation.Paths {
						hostToPathsMap[host] = append(hostToPathsMap[host], path)
					}

					if host == wirecard {
						wirecardPaths = append(wirecardPaths, to.Operation.Paths...)
					}
					totalPaths = append(totalPaths, to.Operation.Paths...)
				}
			}
		}

		targetPolicy.Spec.Rules = []*authpb.Rule{
			{
				From: []*authpb.Rule_From{
					{
						Source: &authpb.Source{
							NotRequestPrincipals: principal.Source.RequestPrincipals,
						},
					},
				},
			},
		}

		var hosts []string
		// Exclude other paths under the specified hosts.
		for host, paths := range hostToPathsMap {
			if host == wirecard {
				continue
			}
			targetPaths := append([]string(nil), paths...)
			targetPaths = append(targetPaths, wirecardPaths...)
			targetPolicy.Spec.Rules[0].To = append(targetPolicy.Spec.Rules[0].To, &authpb.Rule_To{
				Operation: &authpb.Operation{
					Hosts:    []string{host},
					NotPaths: targetPaths,
				},
			})
			hosts = append(hosts, host)
		}

		if len(wirecardPaths) == 0 {
			// We should exclude the other hosts when there is no wirecard host.
			if len(hosts) > 0 {
				targetPolicy.Spec.Rules[0].To = append(targetPolicy.Spec.Rules[0].To, &authpb.Rule_To{
					Operation: &authpb.Operation{
						NotHosts: hosts,
					},
				})
			}
		} else {
			// We should exclude the other paths when there is wirecard host.
			if len(totalPaths) > 0 {
				targetPolicy.Spec.Rules[0].To = append(targetPolicy.Spec.Rules[0].To, &authpb.Rule_To{
					Operation: &authpb.Operation{
						NotPaths: totalPaths,
					},
				})
			}
		}
	}

	return targetPolicy
}

func processPolicyWithExtensionPath(policy model.AuthorizationPolicy) model.AuthorizationPolicy {
	targetPolicy := model.AuthorizationPolicy{
		Name:        policy.Name,
		Namespace:   policy.Namespace,
		Annotations: policy.Annotations,
		Spec:        &authpb.AuthorizationPolicy{},
	}

	if len(policy.Spec.Rules) > 0 {
		principal := policy.Spec.Rules[0].From[0]

		hostToExtensionPathsMap := map[string][]*authpb.StringMatch{}
		var wirecardPaths []*authpb.StringMatch
		var totalPaths []*authpb.StringMatch

		if len(policy.Spec.Rules) > 1 {
			hostPathRule := policy.Spec.Rules[1]
			for _, to := range hostPathRule.To {
				for _, host := range to.Operation.Hosts {
					for _, extensionPath := range to.Operation.ExtensionPaths {
						hostToExtensionPathsMap[host] = append(hostToExtensionPathsMap[host], extensionPath)
					}

					if host == wirecard {
						wirecardPaths = append(wirecardPaths, to.Operation.ExtensionPaths...)
					}

					totalPaths = append(totalPaths, to.Operation.ExtensionPaths...)
				}
			}
		}

		targetPolicy.Spec.Rules = []*authpb.Rule{
			{
				From: []*authpb.Rule_From{
					{
						Source: &authpb.Source{
							NotRequestPrincipals: principal.Source.RequestPrincipals,
						},
					},
				},
			},
		}

		var hosts []string
		// Exclude other paths under the specified hosts.
		for host, paths := range hostToExtensionPathsMap {
			if host == wirecard {
				continue
			}
			targetPaths := append([]*authpb.StringMatch(nil), paths...)
			targetPaths = append(targetPaths, wirecardPaths...)
			targetPolicy.Spec.Rules[0].To = append(targetPolicy.Spec.Rules[0].To, &authpb.Rule_To{
				Operation: &authpb.Operation{
					Hosts:             []string{host},
					ExtensionNotPaths: targetPaths,
				},
			})
			hosts = append(hosts, host)
		}

		if len(wirecardPaths) == 0 {
			// We should exclude the other hosts when there is no wirecard host.
			if len(hosts) > 0 {
				targetPolicy.Spec.Rules[0].To = append(targetPolicy.Spec.Rules[0].To, &authpb.Rule_To{
					Operation: &authpb.Operation{
						NotHosts: hosts,
					},
				})
			}
		} else {
			// We should exclude the other paths when there is wirecard host.
			if len(totalPaths) > 0 {
				targetPolicy.Spec.Rules[0].To = append(targetPolicy.Spec.Rules[0].To, &authpb.Rule_To{
					Operation: &authpb.Operation{
						ExtensionNotPaths: totalPaths,
					},
				})
			}
		}
	}

	return targetPolicy
}

func isUsingExtensionPath(policy model.AuthorizationPolicy) bool {
	if len(policy.Spec.Rules) <= 1 {
		return false
	}

	rule := policy.Spec.Rules[1]
	if len(rule.To) == 0 || rule.To[0].Operation == nil ||
		len(rule.To[0].Operation.ExtensionPaths) == 0 {
		return false
	}

	return true
}
