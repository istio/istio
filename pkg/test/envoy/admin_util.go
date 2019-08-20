//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package envoy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"istio.io/istio/pkg/test/util/retry"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	routeApi "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"

	"github.com/gogo/protobuf/jsonpb"

	"istio.io/istio/istioctl/pkg/util/configdump"
)

const (
	healthCheckTimeout  = 10 * time.Second
	healthCheckInterval = 100 * time.Millisecond
)

// GetServerInfo returns a structure representing a call to /server_info
func GetServerInfo(adminPort int) (*envoyAdmin.ServerInfo, error) {
	buffer, err := doEnvoyGet("server_info", adminPort)
	if err != nil {
		return nil, err
	}

	msg := &envoyAdmin.ServerInfo{}
	if err := jsonpb.Unmarshal(buffer, msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// WaitForHealthCheckLive polls the server info for Envoy and waits for it to transition to "live".
func WaitForHealthCheckLive(adminPort int) error {
	return retry.UntilSuccess(func() error {
		info, err := GetServerInfo(adminPort)
		if err != nil {
			return err
		}

		if info.State != envoyAdmin.ServerInfo_LIVE {
			return fmt.Errorf("envoy not live. Server State: %s", info.State)
		}
		return nil
	}, retry.Delay(healthCheckInterval), retry.Timeout(healthCheckTimeout))
}

// GetConfigDumpStr polls Envoy admin port for the config dump and returns the response as a string.
func GetConfigDumpStr(adminPort int) (string, error) {
	buffer, err := doEnvoyGet("config_dump", adminPort)
	if err != nil {
		return "", err
	}
	return buffer.String(), nil
}

// GetConfigDump polls Envoy admin port for the config dump and returns the response.
func GetConfigDump(adminPort int) (*envoyAdmin.ConfigDump, error) {
	buffer, err := doEnvoyGet("config_dump", adminPort)
	if err != nil {
		return nil, err
	}

	msg := &envoyAdmin.ConfigDump{}
	if err := jsonpb.Unmarshal(buffer, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func doEnvoyGet(path string, adminPort int) (*bytes.Buffer, error) {
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/%s", adminPort, path)
	buffer, err := doHTTPGet(requestURL)
	if err != nil {
		return nil, err
	}
	return buffer, nil
}

// IsClusterPresent inspects the given Envoy config dump, looking for the given cluster
func IsClusterPresent(cfg *envoyAdmin.ConfigDump, clusterName string) bool {
	wrapper := configdump.Wrapper{ConfigDump: cfg}
	clusters, err := wrapper.GetClusterConfigDump()
	if err != nil {
		return false
	}

	for _, c := range clusters.DynamicActiveClusters {
		if c.Cluster == nil {
			continue
		}
		if c.Cluster.Name == clusterName || (c.Cluster.EdsClusterConfig != nil && c.Cluster.EdsClusterConfig.ServiceName == clusterName) {
			return true
		}
	}
	return false
}

// IsOutboundListenerPresent inspects the given Envoy config dump, looking for the given listener.
func IsOutboundListenerPresent(cfg *envoyAdmin.ConfigDump, listenerName string) bool {
	wrapper := configdump.Wrapper{ConfigDump: cfg}
	listeners, err := wrapper.GetListenerConfigDump()
	if err != nil {
		return false
	}

	for _, l := range listeners.DynamicActiveListeners {
		if l.Listener != nil && l.Listener.Name == listenerName {
			return true
		}
	}
	return false
}

// IsOutboundRoutePresent inspects the given Envoy config dump, looking for an outbound route which targets the given cluster.
func IsOutboundRoutePresent(cfg *envoyAdmin.ConfigDump, clusterName string) bool {
	wrapper := configdump.Wrapper{ConfigDump: cfg}
	routes, err := wrapper.GetRouteConfigDump()
	if err != nil {
		return false
	}

	// Look for a route that targets the given outbound cluster.
	for _, r := range routes.DynamicRouteConfigs {
		if r.RouteConfig != nil {
			for _, vh := range r.RouteConfig.VirtualHosts {
				for _, route := range vh.Routes {
					actionRoute, ok := route.Action.(*routeApi.Route_Route)
					if !ok {
						continue
					}

					cluster, ok := actionRoute.Route.ClusterSpecifier.(*routeApi.RouteAction_Cluster)
					if !ok {
						continue
					}

					if cluster.Cluster == clusterName {
						return true
					}
				}
			}
		}
	}
	return false
}

func doHTTPGet(requestURL string) (*bytes.Buffer, error) {
	response, err := http.Get(requestURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status %d", response.StatusCode)
	}

	var b bytes.Buffer
	if _, err := io.Copy(&b, response.Body); err != nil {
		return nil, err
	}
	return &b, nil
}
