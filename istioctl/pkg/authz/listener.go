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
	"regexp"
	"strings"
	"text/tabwriter"

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

var (
	// Matches the policy name in RBAC filter config with format like ns[default]-policy[some-policy]-rule[1].
	re = regexp.MustCompile(`ns\[(.+)\]-policy\[(.+)\]-rule\[(.+)\]`)
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

func parse(listeners []*listener.Listener) *parsedListener {
	parsed := &parsedListener{}
	for _, listener := range listeners {
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
	}

	return parsed
}

func extractName(name string) (string, string) {
	// parts[1] is the namespace, parts[2] is the policy name, parts[3] is the rule index.
	parts := re.FindStringSubmatch(name)
	if len(parts) != 4 {
		log.Errorf("failed to parse policy name: %s", name)
		return "", ""
	}
	return fmt.Sprintf("%s.%s", parts[2], parts[1]), parts[3]
}

// Print prints the AuthorizationPolicy in the listener.
func Print(writer io.Writer, listeners []*listener.Listener) {
	parsed := parse(listeners)
	if parsed == nil {
		return
	}

	actionToPolicy := map[rbacpb.RBAC_Action]map[string]struct{}{}
	policyToRule := map[string]map[string]struct{}{}

	addPolicy := func(action rbacpb.RBAC_Action, name string, rule string) {
		if actionToPolicy[action] == nil {
			actionToPolicy[action] = map[string]struct{}{}
		}
		if policyToRule[name] == nil {
			policyToRule[name] = map[string]struct{}{}
		}
		actionToPolicy[action][name] = struct{}{}
		policyToRule[name][rule] = struct{}{}
	}

	for _, fc := range parsed.filterChains {
		for _, rbacHTTP := range fc.rbacHTTP {
			action := rbacHTTP.GetRules().GetAction()
			for name := range rbacHTTP.GetRules().GetPolicies() {
				nameOfPolicy, indexOfRule := extractName(name)
				addPolicy(action, nameOfPolicy, indexOfRule)
			}
			if len(rbacHTTP.GetRules().GetPolicies()) == 0 {
				addPolicy(action, anonymousName, "0")
			}
		}
		for _, rbacTCP := range fc.rbacTCP {
			action := rbacTCP.GetRules().GetAction()
			for name := range rbacTCP.GetRules().GetPolicies() {
				nameOfPolicy, indexOfRule := extractName(name)
				addPolicy(action, nameOfPolicy, indexOfRule)
			}
			if len(rbacTCP.GetRules().GetPolicies()) == 0 {
				addPolicy(action, anonymousName, "0")
			}
		}
	}

	buf := strings.Builder{}
	buf.WriteString("ACTION\tAuthorizationPolicy\tRULES\n")
	for _, action := range []rbacpb.RBAC_Action{rbacpb.RBAC_DENY, rbacpb.RBAC_ALLOW} {
		if names, ok := actionToPolicy[action]; ok {
			for name := range names {
				buf.WriteString(fmt.Sprintf("%s\t%s\t%d\n", action, name, len(policyToRule[name])))
			}
		}
	}

	w := new(tabwriter.Writer).Init(writer, 0, 8, 3, ' ', 0)
	if _, err := fmt.Fprint(w, buf.String()); err != nil {
		log.Errorf("failed to print output: %s", err)
	}
	_ = w.Flush()
}
