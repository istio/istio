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
	"strings"
	"text/tabwriter"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	envoy_jwt "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/jwt_authn/v3"
	rbac_http_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/rbac/v3"
	hcm_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	rbac_tcp_filter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/rbac/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"

	"istio.io/istio/pilot/pkg/networking/util"
	authn_filter "istio.io/istio/pkg/envoy/config/filter/http/authn/v2alpha1"
	"istio.io/pkg/log"
)

type filterChainMatch struct {
	serverNames []string
	alpn        []string
}

type filterChain struct {
	match      *filterChainMatch
	tlsContext *tls.DownstreamTlsContext

	authN    *authn_filter.FilterConfig
	envoyJWT *envoy_jwt.JwtAuthentication
	rbacHTTP *rbac_http_filter.RBAC
	rbacTCP  *rbac_tcp_filter.RBAC

	routeHTTP string
}

type ParsedListener struct {
	name         string
	ip           string
	port         string
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

// ParseListener parses the envoy listener config by extracting the auth related config.
func ParseListener(listener *listener.Listener) *ParsedListener {
	parsedListener := &ParsedListener{
		name: listener.Name,
		ip:   listener.Address.GetSocketAddress().Address,
		port: fmt.Sprintf("%d", listener.Address.GetSocketAddress().GetPortValue()),
	}

	for _, fc := range listener.FilterChains {
		tlsContext := &tls.DownstreamTlsContext{}
		if fc.TransportSocket != nil && fc.TransportSocket.Name == util.EnvoyTLSSocketName {
			if err := ptypes.UnmarshalAny(fc.TransportSocket.GetTypedConfig(), tlsContext); err != nil {
				log.Warnf("failed to unmarshal tls settings: %v", err)
			}
		}
		parsedFC := &filterChain{tlsContext: tlsContext}
		for _, filter := range fc.Filters {
			switch filter.Name {
			case wellknown.HTTPConnectionManager:
				if cm := getHTTPConnectionManager(filter); cm != nil {
					for _, httpFilter := range cm.GetHttpFilters() {
						switch httpFilter.GetName() {
						case "istio_authn":
							authN := &authn_filter.FilterConfig{}
							if err := getHTTPFilterConfig(httpFilter, authN); err != nil {
								log.Errorf("found AuthN filter but failed to parse: %s", err)
							} else {
								parsedFC.authN = authN
							}
						case "envoy.filters.http.jwt_authn":
							jwt := &envoy_jwt.JwtAuthentication{}
							if err := getHTTPFilterConfig(httpFilter, jwt); err != nil {
								log.Errorf("found JWT filter but failed to parse: %s", err)
							} else {
								parsedFC.envoyJWT = jwt
							}
						case wellknown.HTTPRoleBasedAccessControl:
							rbacHTTP := &rbac_http_filter.RBAC{}
							if err := getHTTPFilterConfig(httpFilter, rbacHTTP); err != nil {
								log.Errorf("found RBAC HTTP filter but failed to parse: %s", err)
							} else {
								parsedFC.rbacHTTP = rbacHTTP
							}
						}
					}
					switch r := cm.GetRouteSpecifier().(type) {
					case *hcm_filter.HttpConnectionManager_RouteConfig:
						parsedFC.routeHTTP = r.RouteConfig.Name
					case *hcm_filter.HttpConnectionManager_Rds:
						parsedFC.routeHTTP = r.Rds.RouteConfigName
					}
				}
			case wellknown.RoleBasedAccessControl:
				rbacTCP := &rbac_tcp_filter.RBAC{}
				if err := getFilterConfig(filter, rbacTCP); err != nil {
					log.Errorf("found RBAC network filter but failed to parse: %s", err)
				} else {
					parsedFC.rbacTCP = rbacTCP
				}
			}
		}

		if fc.FilterChainMatch != nil {
			parsedFC.match = &filterChainMatch{
				serverNames: fc.FilterChainMatch.GetServerNames(),
				alpn:        fc.FilterChainMatch.GetApplicationProtocols(),
			}
		}
		parsedListener.filterChains = append(parsedListener.filterChains, parsedFC)
	}

	return parsedListener
}

func (l *ParsedListener) print(w io.Writer, printAll bool) {
	for i, fc := range l.filterChains {
		listenerName := l.name
		if len(l.filterChains) > 1 {
			listenerName = fmt.Sprintf("%s[%d]", listenerName, i)
		}
		sni := ""
		alpn := ""
		if fc.match != nil {
			sni = strings.Join(fc.match.serverNames, ",")
			alpn = strings.Join(fc.match.alpn, ",")
		}

		cert := "none"
		mTLSEnabled := "no"
		if tlscontext := fc.tlsContext; tlscontext != nil {
			cert = getCertificate(tlscontext.GetCommonTlsContext())
			if tlscontext.RequireClientCertificate.GetValue() {
				mTLSEnabled = "yes"
			}
		}

		mTLSMode := "none"
		if fc.authN != nil {
			modes := make([]string, 0)
			if p := fc.authN.Policy; p != nil {
				for _, peer := range p.Peers {
					if m := peer.GetMtls(); m != nil {
						modes = append(modes, m.GetMode().String())
					}
				}
			}
			if len(modes) != 0 {
				mTLSMode = strings.Join(modes, ",")
			}
		}
		mTLS := fmt.Sprintf("%s (%s)", mTLSEnabled, mTLSMode)

		rbacPolicy := "no (none)"
		if fc.rbacHTTP != nil || fc.rbacTCP != nil {
			rbacPolicy = "yes (none)"
			rules := make([]string, 0)
			for p := range fc.rbacHTTP.GetRules().GetPolicies() {
				rules = append(rules, p)
			}
			for p := range fc.rbacTCP.GetRules().GetPolicies() {
				rules = append(rules, p)
			}
			if len(rules) != 0 {
				rbacPolicy = fmt.Sprintf("yes (%d: %s)", len(rules), strings.Join(rules, ", "))
			}
		}

		var err error
		if printAll {
			_, err = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				listenerName, fc.routeHTTP, sni, alpn, cert, mTLS, rbacPolicy)
		} else {
			_, err = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				listenerName, cert, mTLS, rbacPolicy)
		}
		if err != nil {
			log.Errorf("failed to print output: %s", err)
			return
		}
	}
}

func PrintParsedListeners(writer io.Writer, parsedListeners []*ParsedListener, printAll bool) {
	w := new(tabwriter.Writer).Init(writer, 0, 8, 5, ' ', 0)
	col := "LISTENER[FilterChain]\tHTTP ROUTE\tSNI\tALPN\tCERTIFICATE\tmTLS (MODE)\tAuthZ (RULES)"
	if !printAll {
		col = "LISTENER[FilterChain]\tCERTIFICATE\tmTLS (MODE)\tAuthZ (RULES)"
	}

	if _, err := fmt.Fprintln(w, col); err != nil {
		log.Errorf("failed to print output: %s", err)
		return
	}
	for _, l := range parsedListeners {
		l.print(w, printAll)
	}
	_ = w.Flush()
}
