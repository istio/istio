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

package auth

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	v2 "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	rbac_http_filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/http/rbac/v2"
	hcm_filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	rbac_tcp_filter "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/rbac/v2"
	"github.com/envoyproxy/go-control-plane/pkg/util"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	authn_filter "istio.io/api/envoy/config/filter/http/authn/v2alpha1"
	jwt_filter "istio.io/api/envoy/config/filter/http/jwt_auth/v2alpha1"

	"istio.io/istio/pkg/log"
)

type filterChainMatch struct {
	serverNames []string
	alpn        []string
}

type filterChain struct {
	match      *filterChainMatch
	tlsContext *auth.DownstreamTlsContext

	authN    *authn_filter.FilterConfig
	jwt      *jwt_filter.JwtAuthentication
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

func getFilterConfig(filter listener.Filter, out proto.Message) error {
	switch c := filter.ConfigType.(type) {
	case *listener.Filter_Config:
		if err := util.StructToMessage(c.Config, out); err != nil {
			return err
		}
	case *listener.Filter_TypedConfig:
		if err := types.UnmarshalAny(c.TypedConfig, out); err != nil {
			return err
		}
	}
	return nil
}

func getHTTPConnectionManager(filter listener.Filter) *hcm_filter.HttpConnectionManager {
	cm := &hcm_filter.HttpConnectionManager{}
	if err := getFilterConfig(filter, cm); err != nil {
		log.Errorf("failed to get HTTP connection manager config: %s", err)
		return nil
	}
	return cm
}

func getHTTPFilterConfig(filter *hcm_filter.HttpFilter, out proto.Message) error {
	switch c := filter.ConfigType.(type) {
	case *hcm_filter.HttpFilter_Config:
		if err := util.StructToMessage(c.Config, out); err != nil {
			return err
		}
	case *hcm_filter.HttpFilter_TypedConfig:
		if err := types.UnmarshalAny(c.TypedConfig, out); err != nil {
			return err
		}
	}
	return nil
}

// ParseListener parses the envoy listener config by extracting the auth related config.
func ParseListener(listener *v2.Listener) *ParsedListener {
	parsedListener := &ParsedListener{
		name: listener.Name,
		ip:   listener.Address.GetSocketAddress().Address,
		port: fmt.Sprintf("%d", listener.Address.GetSocketAddress().GetPortValue()),
	}

	for _, fc := range listener.FilterChains {
		parsedFC := &filterChain{tlsContext: fc.TlsContext}
		for _, filter := range fc.Filters {
			switch filter.Name {
			case "envoy.http_connection_manager":
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
						case "jwt-auth":
							jwt := &jwt_filter.JwtAuthentication{}
							if err := getHTTPFilterConfig(httpFilter, jwt); err != nil {
								log.Errorf("found JWT filter but failed to parse: %s", err)
							} else {
								parsedFC.jwt = jwt
							}
						case "envoy.filters.http.rbac":
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
			case "envoy.filters.network.rbac":
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
		if tls := fc.tlsContext; tls != nil {
			cert = getCertificate(tls.GetCommonTlsContext())
			if tls.RequireClientCertificate.GetValue() {
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

		jwtPolicy := "no (none)"
		if fc.jwt != nil {
			jwtPolicy = "yes (none)"
			issuers := make([]string, 0)
			for _, rule := range fc.jwt.Rules {
				issuers = append(issuers, rule.Issuer)
			}
			if len(issuers) != 0 {
				jwtPolicy = fmt.Sprintf("yes (%s)", strings.Join(issuers, ","))
			}
		}

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
				rbacPolicy = fmt.Sprintf("yes (%s)", strings.Join(rules, ","))
			}
		}

		var err error
		if printAll {
			_, err = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				listenerName, fc.routeHTTP, sni, alpn, cert, mTLS, jwtPolicy, rbacPolicy)
		} else {
			_, err = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				listenerName, cert, mTLS, jwtPolicy, rbacPolicy)
		}
		if err != nil {
			log.Errorf("failed to print output: %s", err)
			return
		}
	}
}

func PrintParsedListeners(writer io.Writer, parsedListeners []*ParsedListener, printAll bool) {
	w := new(tabwriter.Writer).Init(writer, 0, 8, 5, ' ', 0)
	col := "LISTENER\tHTTP ROUTE\tSNI\tALPN\tCERTIFICATE\tmTLS (MODE)\tJWT (ISSUERS)\tRBAC (RULES)"
	if !printAll {
		col = "LISTENER\tCERTIFICATE\tmTLS (MODE)\tJWT (ISSUERS)\tRBAC (RULES)"
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
