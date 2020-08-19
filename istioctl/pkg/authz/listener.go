// Copyright Istio Authors
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

package authz

import (
	"fmt"
	"io"
	"sort"
	"strings"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	rbac_http_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	hcm_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	rbac_tcp_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"istio.io/pkg/log"
)

const (
	anonymousName = "_anonymous_match_nothing_"
)

type filterChain struct {
	rbacHTTP []*rbac_http_filter.RBAC
	rbacTCP  []*rbac_tcp_filter.RBAC
}

type parsedListener struct {
	filterChains []*filterChain
}

func getFilterConfig(filter *listener.Filter, out proto.Message) error {
	switch c := filter.ConfigType.(type) {
	case *listener.Filter_TypedConfig:
		if err := ptypes.UnmarshalAny(c.TypedConfig, out); err != nil {
			return err
		}
	}
	return nil
}

func getHTTPConnectionManager(filter *listener.Filter) *hcm_filter.HttpConnectionManager {
	cm := &hcm_filter.HttpConnectionManager{}
	if err := getFilterConfig(filter, cm); err != nil {
		log.Errorf("failed to get HTTP connection manager config: %s", err)
		return nil
	}
	return cm
}

func getHTTPFilterConfig(filter *hcm_filter.HttpFilter, out proto.Message) error {
	switch c := filter.ConfigType.(type) {
	case *hcm_filter.HttpFilter_TypedConfig:
		if err := ptypes.UnmarshalAny(c.TypedConfig, out); err != nil {
			return err
		}
	}
	return nil
}

func parse(listener *listener.Listener) *parsedListener {
	parsed := &parsedListener{}
	for _, fc := range listener.FilterChains {
		parsedFC := &filterChain{}
		for _, filter := range fc.Filters {
			switch filter.Name {
			case wellknown.HTTPConnectionManager, "envoy.http_connection_manager":
				if cm := getHTTPConnectionManager(filter); cm != nil {
					for _, httpFilter := range cm.GetHttpFilters() {
						switch httpFilter.GetName() {
						case wellknown.HTTPRoleBasedAccessControl:
							rbacHTTP := &rbac_http_filter.RBAC{}
							if err := getHTTPFilterConfig(httpFilter, rbacHTTP); err != nil {
								log.Errorf("found RBAC HTTP filter but failed to parse: %s", err)
							} else {
								parsedFC.rbacHTTP = append(parsedFC.rbacHTTP, rbacHTTP)
							}
						}
					}
				}
			case wellknown.RoleBasedAccessControl:
				rbacTCP := &rbac_tcp_filter.RBAC{}
				if err := getFilterConfig(filter, rbacTCP); err != nil {
					log.Errorf("found RBAC network filter but failed to parse: %s", err)
				} else {
					parsedFC.rbacTCP = append(parsedFC.rbacTCP, rbacTCP)
				}
			}
		}

		parsed.filterChains = append(parsed.filterChains, parsedFC)
	}

	return parsed
}

// Print prints the AuthorizationPolicy in the listener.
func Print(writer io.Writer, listener *listener.Listener) {
	parsed := parse(listener)
	if parsed == nil {
		return
	}

	policies := map[rbacpb.RBAC_Action]map[string]struct{}{}
	for _, fc := range parsed.filterChains {
		for _, rbacHTTP := range fc.rbacHTTP {
			action := rbacHTTP.GetRules().GetAction()
			if policies[action] == nil {
				policies[action] = map[string]struct{}{}
			}
			for name := range rbacHTTP.GetRules().GetPolicies() {
				policies[action][name] = struct{}{}
			}
			if len(rbacHTTP.GetRules().GetPolicies()) == 0 {
				policies[action][anonymousName] = struct{}{}
			}
		}
		for _, rbacTCP := range fc.rbacTCP {
			action := rbacTCP.GetRules().GetAction()
			if policies[action] == nil {
				policies[action] = map[string]struct{}{}
			}
			for name := range rbacTCP.GetRules().GetPolicies() {
				policies[action][name] = struct{}{}
			}
			if len(rbacTCP.GetRules().GetPolicies()) == 0 {
				policies[action][anonymousName] = struct{}{}
			}
		}
	}

	var actionBreakdown []string
	var allPolicies []string
	for _, action := range []rbacpb.RBAC_Action{rbacpb.RBAC_DENY, rbacpb.RBAC_ALLOW, rbacpb.RBAC_LOG} {
		if names, ok := policies[action]; ok {
			actionBreakdown = append(actionBreakdown, fmt.Sprintf("%d %s rule(s)", len(names), action))
			for name := range names {
				allPolicies = append(allPolicies, fmt.Sprintf("- [%5s] %s", action, name))
			}
		}
	}
	buf := strings.Builder{}
	total := len(allPolicies)
	sort.Strings(allPolicies)
	buf.WriteString(fmt.Sprintf("Found %d Rule(s) of AuthorizationPolicy", total))
	if total > 0 {
		buf.WriteString(fmt.Sprintf(" (%s)\n", strings.Join(actionBreakdown, ", ")))
		buf.WriteString(strings.Join(allPolicies, "\n"))
	}
	buf.WriteString("\n")
	fmt.Fprint(writer, buf.String())
}
