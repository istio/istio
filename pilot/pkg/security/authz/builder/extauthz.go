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

package builder

import (
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"
	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	rbacpb "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v3"
	extauthzhttp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/ext_authz/v3"
	extauthztcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/ext_authz/v3"
	envoy_type_matcher_v3 "github.com/envoyproxy/go-control-plane/envoy/type/matcher/v3"
	envoytypev3 "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/golang/protobuf/ptypes/duration"
	"github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/security"
	authzmodel "istio.io/istio/pilot/pkg/security/authz/model"
)

const (
	extAuthzMatchPrefix = "istio-ext-authz"
)

var (
	rbacPolicyMatchAll = &rbacpb.Policy{
		Permissions: []*rbacpb.Permission{{Rule: &rbacpb.Permission_Any{Any: true}}},
		Principals:  []*rbacpb.Principal{{Identifier: &rbacpb.Principal_Any{Any: true}}},
	}
	rbacDefaultDenyAll = &rbacpb.RBAC{
		Action: rbacpb.RBAC_DENY,
		Policies: map[string]*rbacpb.Policy{
			"default-deny-all-due-to-bad-CUSTOM-action": rbacPolicyMatchAll,
		},
	}
	supportedStatus = func() []int {
		var supported []int
		for code := range envoytypev3.StatusCode_name {
			supported = append(supported, int(code))
		}
		sort.Ints(supported)
		return supported
	}()
)

type builtExtAuthz struct {
	http *extauthzhttp.ExtAuthz
	tcp  *extauthztcp.ExtAuthz
	err  error
}

func processExtensionProvider(in *plugin.InputParams) map[string]*builtExtAuthz {
	resolved := map[string]*builtExtAuthz{}
	for i, config := range in.Push.Mesh.ExtensionProviders {
		var errs error
		if config.Name == "" {
			errs = multierror.Append(errs, fmt.Errorf("extension provider name must not be empty, found empty at index: %d", i))
		} else if _, found := resolved[config.Name]; found {
			errs = multierror.Append(errs, fmt.Errorf("extension provider name must be unique, found duplicate: %s", config.Name))
		}
		var parsed *builtExtAuthz
		var err error
		// TODO(yangminzhu): Refactor and cache the ext_authz config.
		switch p := config.Provider.(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp:
			parsed, err = buildExtAuthzHTTP(in, p.EnvoyExtAuthzHttp)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc:
			parsed, err = buildExtAuthzGRPC(in, p.EnvoyExtAuthzGrpc)
		default:
			err = fmt.Errorf("unsupported extension provider: %s", config.Name)
		}
		if err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("failed to parse extension provider %q:", config.Name)))
		}
		if parsed == nil {
			parsed = &builtExtAuthz{}
		}
		parsed.err = errs
		resolved[config.Name] = parsed
	}

	if authzLog.DebugEnabled() {
		authzLog.Debugf("Resolved extension providers: %v", spew.Sdump(resolved))
	}
	return resolved
}

func notAllTheSame(names []string) bool {
	for i := 1; i < len(names); i++ {
		if names[i-1] != names[i] {
			return true
		}
	}
	return false
}

func getExtAuthz(resolved map[string]*builtExtAuthz, providers []string) (*builtExtAuthz, error) {
	if resolved == nil {
		return nil, fmt.Errorf("extension provider is either invalid or undefined")
	}
	if len(providers) < 1 {
		return nil, fmt.Errorf("no provider specified in authorization policy")
	}
	if notAllTheSame(providers) {
		return nil, fmt.Errorf("only 1 provider can be used per workload, found multiple providers: %v", providers)
	}

	provider := providers[0]
	ret, found := resolved[provider]
	if !found {
		var li []string
		for p := range resolved {
			li = append(li, p)
		}
		return nil, fmt.Errorf("available providers are %v but found %q", li, provider)
	} else if ret.err != nil {
		return nil, fmt.Errorf("found errors in provider %s: %v", provider, ret.err)
	}

	return ret, nil
}

func buildExtAuthzHTTP(in *plugin.InputParams, config *meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationHttpProvider) (*builtExtAuthz, error) {
	var errs error
	port, err := parsePort(config.Port)
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	hostname, cluster, err := security.LookupCluster(in.Push, config.Service, port)
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	status, err := parseStatusOnError(config.StatusOnError)
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	if config.PathPrefix != "" {
		if _, err := url.Parse(config.PathPrefix); err != nil {
			errs = multierror.Append(errs, multierror.Prefix(err, fmt.Sprintf("invalid pathPrefix %q:", config.PathPrefix)))
		}
		if !strings.HasPrefix(config.PathPrefix, "/") {
			errs = multierror.Append(errs, fmt.Errorf("pathPrefix must begin with `/`, found: %q", config.PathPrefix))
		}
	}
	if errs != nil {
		return nil, errs
	}

	return generateHTTPConfig(hostname, cluster, status, config), nil
}

func buildExtAuthzGRPC(in *plugin.InputParams, config *meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider) (*builtExtAuthz, error) {
	var errs error
	port, err := parsePort(config.Port)
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	_, cluster, err := security.LookupCluster(in.Push, config.Service, port)
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	status, err := parseStatusOnError(config.StatusOnError)
	if err != nil {
		errs = multierror.Append(errs, err)
	}
	if errs != nil {
		return nil, errs
	}

	return generateGRPCConfig(cluster, config.FailOpen, status), nil
}

func parsePort(port uint32) (int, error) {
	if 1 <= port && port <= 65535 {
		return int(port), nil
	}
	return 0, fmt.Errorf("port must be in the range [1, 65535], found: %d", port)
}

func parseStatusOnError(status string) (*envoytypev3.HttpStatus, error) {
	if status == "" {
		return nil, nil
	}
	code, err := strconv.ParseInt(status, 10, 32)
	if err != nil {
		return nil, multierror.Prefix(err, fmt.Sprintf("invalid statusOnError %q:", status))
	}
	if _, found := envoytypev3.StatusCode_name[int32(code)]; !found {
		return nil, fmt.Errorf("unsupported statusOnError %s, supported values: %v", status, supportedStatus)
	}
	return &envoytypev3.HttpStatus{Code: envoytypev3.StatusCode(code)}, nil
}

func generateHTTPConfig(hostname, cluster string, status *envoytypev3.HttpStatus,
	config *meshconfig.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationHttpProvider) *builtExtAuthz {
	service := &extauthzhttp.HttpService{
		PathPrefix: config.PathPrefix,
		ServerUri: &envoy_config_core_v3.HttpUri{
			// Timeout is required. Use a large value as a placeholder and so that the timeout in DestinationRule (should
			// be much smaller) can be used to control the real timeout.
			// TODO(yangminzhu): Revisit the default and investigate if we need to expose it in the MeshConfig.
			Timeout: &duration.Duration{Seconds: 600},
			// Uri is required but actually not used in the ext_authz filter.
			Uri: fmt.Sprintf("http://%s", hostname),
			HttpUpstreamType: &envoy_config_core_v3.HttpUri_Cluster{
				Cluster: cluster,
			},
		},
	}
	if headers := generateHeaders(config.IncludeHeadersInCheck); headers != nil {
		service.AuthorizationRequest = &extauthzhttp.AuthorizationRequest{
			AllowedHeaders: headers,
		}
	}
	if len(config.HeadersToUpstreamOnAllow) > 0 || len(config.HeadersToDownstreamOnDeny) > 0 {
		service.AuthorizationResponse = &extauthzhttp.AuthorizationResponse{
			AllowedUpstreamHeaders: generateHeaders(config.HeadersToUpstreamOnAllow),
			AllowedClientHeaders:   generateHeaders(config.HeadersToDownstreamOnDeny),
		}
	}
	http := &extauthzhttp.ExtAuthz{
		StatusOnError:       status,
		FailureModeAllow:    config.FailOpen,
		TransportApiVersion: envoy_config_core_v3.ApiVersion_V3,
		Services: &extauthzhttp.ExtAuthz_HttpService{
			HttpService: service,
		},
		FilterEnabledMetadata: generateFilterMatcher(authzmodel.RBACHTTPFilterName),
	}
	return &builtExtAuthz{http: http}
}

func generateGRPCConfig(cluster string, failOpen bool, status *envoytypev3.HttpStatus) *builtExtAuthz {
	// The cluster includes the character `|` that is invalid in gRPC authority header and will cause the connection
	// rejected in the server side, replace it with a valid character and set in authority otherwise ext_authz will
	// use the cluster name as default authority.
	authority := strings.ReplaceAll(cluster, "|", "_.")
	grpc := &envoy_config_core_v3.GrpcService{
		TargetSpecifier: &envoy_config_core_v3.GrpcService_EnvoyGrpc_{
			EnvoyGrpc: &envoy_config_core_v3.GrpcService_EnvoyGrpc{
				ClusterName: cluster,
				Authority:   authority,
			},
		},
		Timeout: &duration.Duration{Seconds: 600},
	}
	http := &extauthzhttp.ExtAuthz{
		StatusOnError:    status,
		FailureModeAllow: failOpen,
		Services: &extauthzhttp.ExtAuthz_GrpcService{
			GrpcService: grpc,
		},
		FilterEnabledMetadata: generateFilterMatcher(authzmodel.RBACHTTPFilterName),
		TransportApiVersion:   envoy_config_core_v3.ApiVersion_V3,
	}
	tcp := &extauthztcp.ExtAuthz{
		StatPrefix:            "tcp.",
		FailureModeAllow:      failOpen,
		TransportApiVersion:   envoy_config_core_v3.ApiVersion_V3,
		GrpcService:           grpc,
		FilterEnabledMetadata: generateFilterMatcher(authzmodel.RBACTCPFilterName),
	}
	return &builtExtAuthz{http: http, tcp: tcp}
}

func generateHeaders(headers []string) *envoy_type_matcher_v3.ListStringMatcher {
	if len(headers) == 0 {
		return nil
	}
	var patterns []*envoy_type_matcher_v3.StringMatcher
	for _, header := range headers {
		patterns = append(patterns, &envoy_type_matcher_v3.StringMatcher{
			MatchPattern: &envoy_type_matcher_v3.StringMatcher_Exact{
				Exact: header,
			},
		})
	}
	return &envoy_type_matcher_v3.ListStringMatcher{Patterns: patterns}
}

func generateFilterMatcher(name string) *envoy_type_matcher_v3.MetadataMatcher {
	return &envoy_type_matcher_v3.MetadataMatcher{
		Filter: name,
		Path: []*envoy_type_matcher_v3.MetadataMatcher_PathSegment{
			{
				Segment: &envoy_type_matcher_v3.MetadataMatcher_PathSegment_Key{
					Key: "shadow_effective_policy_id",
				},
			},
		},
		Value: &envoy_type_matcher_v3.ValueMatcher{
			MatchPattern: &envoy_type_matcher_v3.ValueMatcher_StringMatch{
				StringMatch: &envoy_type_matcher_v3.StringMatcher{
					MatchPattern: &envoy_type_matcher_v3.StringMatcher_Prefix{
						Prefix: extAuthzMatchPrefix,
					},
				},
			},
		},
	}
}
