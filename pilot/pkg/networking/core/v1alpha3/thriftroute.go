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

package v1alpha3

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	thrift "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/thrift_proxy/v3"

	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
)

// buildDefaultThriftInboundRoute builds a default inbound route.
func buildDefaultThriftRoute(clusterName, rateLimitClusterName string) *thrift.Route {
	var rateLimits []*route.RateLimit
	if rateLimitClusterName != "" {
		rateLimits = []*route.RateLimit{
			{
				Actions: []*route.RateLimit_Action{
					{
						ActionSpecifier: &route.RateLimit_Action_SourceCluster_{
							// Automatically populated
							SourceCluster: &route.RateLimit_Action_SourceCluster{},
						},
					},
				},
			},
		}
	}

	return &thrift.Route{
		Match: &thrift.RouteMatch{
			MatchSpecifier: &thrift.RouteMatch_MethodName{
				MethodName: "",
			},
		},

		// Specifies a set of rate limit configurations that could be applied to the route.
		// N.B. Thrift service or method name matching can be achieved by specifying a RequestHeaders
		// action with the header name ":method-name".
		Route: &thrift.RouteAction{
			ClusterSpecifier: &thrift.RouteAction_Cluster{
				Cluster: clusterName,
			},
			RateLimits: rateLimits,
		},
	}
}

// Builds the route config with a single blank method route on the inbound path.
// We route inbound and outbound identically.
func (configgen *ConfigGeneratorImpl) buildSidecarThriftRouteConfig(clusterName, rateLimitURL string) *thrift.RouteConfiguration {

	rlsClusterName, err := thriftRLSClusterNameFromAuthority(rateLimitURL)
	if err != nil {
		rlsClusterName = ""
	}

	routes := []*thrift.Route{
		buildDefaultThriftRoute(clusterName, rlsClusterName),
	}

	return &thrift.RouteConfiguration{
		Name:   clusterName,
		Routes: routes,
	}
}

// Build a cluster name from an authority (host[:port]) string. If an error is
// encountered, an empty string is returned as the cluster name.
func thriftRLSClusterNameFromAuthority(authority string) (string, error) {
	rlsPort := 8081

	if authority == "" {
		return "", errors.New("empty url")
	}

	components := strings.Split(authority, ":")
	if len(components) < 2 {
		log.Debugf("using default port to parse rate limit port from authority (using %v default): %s", rlsPort, authority)
	} else if len(components) > 2 {
		return "", fmt.Errorf("authority had too many components: %v", authority)
	} else {
		p, err := strconv.Atoi(components[1])
		if err != nil {
			return "", fmt.Errorf("unable to parse port provided in authority: %v", authority)
		}
		rlsPort = p
	}

	return model.BuildSubsetKey(model.TrafficDirectionOutbound, "", host.Name(components[0]), rlsPort), nil
}
