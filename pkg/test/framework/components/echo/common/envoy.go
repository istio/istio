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

package common

import (
	"errors"
	"fmt"
	"strings"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/jsonpb"

	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/structpath"
)

const (
	// DefaultTimeout the default timeout for the entire retry operation
	defaultConfigTimeout = time.Second * 30

	// DefaultDelay the default delay between successive retry attempts
	defaultConfigDelay = time.Second * 2
)

// ConfigFetchFunc retrieves the config dump from Envoy.
type ConfigFetchFunc func() (*envoyAdmin.ConfigDump, error)

// ConfigAcceptFunc evaluates the Envoy config dump and either accept/reject it. This is used
// by WaitForConfig to control the retry loop. If an error is returned, a retry will be attempted.
// Otherwise the loop is immediately terminated with an error if rejected or none if accepted.
type ConfigAcceptFunc func(*envoyAdmin.ConfigDump) (bool, error)

func WaitForConfig(fetch ConfigFetchFunc, accept ConfigAcceptFunc, options ...retry.Option) error {
	options = append([]retry.Option{retry.Delay(defaultConfigDelay), retry.Timeout(defaultConfigTimeout)}, options...)

	var cfg *envoyAdmin.ConfigDump
	_, err := retry.Do(func() (result interface{}, completed bool, err error) {
		cfg, err = fetch()
		if err != nil {
			if strings.Contains(err.Error(), "could not resolve Any message type") {
				// Unable to parse an Any in the message, likely due to missing imports.
				// This is not a recoverable error.
				return nil, true, err
			}
			return nil, false, err
		}

		accepted, err := accept(cfg)
		if err != nil {
			// Accept returned an error - retry.
			return nil, false, err
		}

		if accepted {
			// The configuration was accepted.
			return nil, true, nil
		}

		// The configuration was rejected, don't try again.
		return nil, true, errors.New("envoy config rejected")
	}, options...)

	if err != nil {
		configDumpStr := "nil"
		if cfg != nil {
			m := jsonpb.Marshaler{
				Indent: "  ",
			}
			if out, err := m.MarshalToString(cfg); err == nil {
				configDumpStr = out
			}
		}

		return fmt.Errorf("failed waiting for Envoy configuration: %v. Last config_dump:\n%s", err, configDumpStr)
	}
	return nil
}

// OutboundConfigAcceptFunc returns a function that accepts Envoy configuration if it contains
// outbound configuration for all of the given instances.
func OutboundConfigAcceptFunc(source echo.Instance, targets ...echo.Instance) ConfigAcceptFunc {
	return func(cfg *envoyAdmin.ConfigDump) (bool, error) {
		validator := structpath.ForProto(cfg)

		for _, target := range targets {
			for _, port := range target.Config().Ports {
				// Ensure that we have an outbound configuration for the target port.
				if err := CheckOutboundConfig(source, target, port, validator); err != nil {
					return false, err
				}
			}
		}

		return true, nil
	}
}

// CheckOutboundConfig checks the Envoy config dump for outbound configuration to the given target.
func CheckOutboundConfig(source echo.Instance, target echo.Instance, port echo.Port, validator *structpath.Instance) error {
	// Verify that we have an outbound cluster for the target.
	clusterName := clusterName(target, port)
	if err := validator.
		Exists("{.configs[*].dynamicActiveClusters[?(@.cluster.name == '%s')]}", clusterName).
		Check(); err != nil {
		if err := validator.
			Exists("{.configs[*].dynamicActiveClusters[?(@.cluster.edsClusterConfig.serviceName == '%s')]}", clusterName).
			Check(); err != nil {
			return err
		}
	}

	// For HTTP endpoints, verify that we have a route configured.
	if port.Protocol.IsHTTP() {
		rname := routeName(target, port)
		return validator.
			Select("{.configs[*].dynamicRouteConfigs[*].routeConfig.virtualHosts[?(@.name == '%s')]}", rname).
			Exists("{.routes[?(@.route.cluster == '%s')]}", clusterName).
			Check()
	}

	if !target.Config().Headless && source.Config().Cluster.Name() == target.Config().Cluster.Name() {
		// TCP case: Make sure we have an outbound listener configured.
		listenerName := listenerName(target.Address(), port)
		return validator.
			Exists("{.configs[*].dynamicListeners[?(@.name == '%s')]}", listenerName).
			Check()
	}
	return nil
}

func clusterName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("outbound|%d||%s.%s.svc.%s", port.ServicePort, cfg.Service, cfg.Namespace.Name(), cfg.Domain)
}

func routeName(target echo.Instance, port echo.Port) string {
	cfg := target.Config()
	return fmt.Sprintf("%s:%d", cfg.FQDN(), port.ServicePort)
}

func listenerName(address string, port echo.Port) string {
	return fmt.Sprintf("%s_%d", address, port.ServicePort)
}
