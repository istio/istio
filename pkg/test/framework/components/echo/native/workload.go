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

package native

import (
	"errors"
	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/echo/server"
	"istio.io/istio/pkg/test/envoy"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/components/pilot"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	envoyLogLovel = envoy.LogLevelWarning
)

var _ echo.Workload = &workload{}

type workload struct {
	*client.Instance

	discoveryFilter discoveryFilter
	echoServer      *server.Instance
	sidecar         *sidecar
}

func newWorkload(ctx resource.Context, cfg *echo.Config) (w *workload, err error) {
	env := ctx.Environment().(*native.Environment)

	out := &workload{}

	defer func() {
		if err != nil {
			_ = out.Close()
		}
	}()

	// Convert the configured ports for the echo application. Ignore any specified port numbers.
	appPorts := make(model.PortList, 0, len(cfg.Ports))
	for _, p := range cfg.Ports {
		appPorts = append(appPorts, &model.Port{
			Name:     p.Name,
			Protocol: p.Protocol,
		})
	}

	out.echoServer = server.New(server.Config{
		Ports:   appPorts,
		Version: cfg.Version,
	})

	// Create and start the Echo application
	if err = out.echoServer.Start(); err != nil {
		return nil, err
	}

	if cfg.Sidecar {
		// Using a sidecar Envoy proxy. Need to wire up a custom discovery filter and start Envoy...

		if cfg.Galley == nil {
			return nil, fmt.Errorf("galley must be provided when running echo with a sidecar")
		}
		if cfg.Pilot == nil {
			return nil, fmt.Errorf("pilot must be provided when running echo with a sidecar")
		}

		// Get Pilot's discovery address.
		pilotDiscoveryAddress := cfg.Pilot.(pilot.Native).GetDiscoveryAddress()

		// Create the discovery filter that modifies the XDS from Pilot to allow the application run
		// run natively.
		out.discoveryFilter, err = newDiscoverFilter(pilotDiscoveryAddress.String(), env.PortManager)
		if err != nil {
			return nil, err
		}

		// Create a temp directory for the Envoy boostrap YAML file.
		tmpDir, err := ctx.CreateTmpDirectory("echo_" + cfg.Service)
		if err != nil {
			return nil, err
		}

		// Create an start a new sidecar.
		if out.sidecar, err = newSidecar(sidecarConfig{
			discoveryAddress: out.discoveryFilter.GetDiscoveryAddress(),
			portManager:      env.PortManager,
			outDir:           tmpDir,
			domain:           env.Domain,
			service:          cfg.Service,
			envoyLogLevel:    envoyLogLovel,
			namespace:        cfg.Namespace.Name(),
			servicePorts:     out.echoServer.Ports,
		}); err != nil {
			return nil, err
		}

		// Apply the service config to Galley.
		svcCfg := serviceConfig{
			service:  cfg.Service,
			ns:       cfg.Namespace,
			domain:   env.Domain,
			version:  cfg.Version,
			ports:    out.sidecar.GetPorts(),
			locality: cfg.Locality,
		}
		if _, err = svcCfg.applyTo(cfg.Galley); err != nil {
			return nil, err
		}

		// Update the ports in the configuration to reflect the port mapping between Envoy and the Application.
		cfg.Ports = out.sidecar.GetPorts()
	} else {
		// No sidecar case is simple: just use the application ports directly ...

		// Update the configuration with the ports assigned by the application.
		cfg.Ports = cfg.Ports[:0]
		for _, p := range out.echoServer.Ports {
			cfg.Ports = append(cfg.Ports, echo.Port{
				Name:         p.Name,
				Protocol:     p.Protocol,
				ServicePort:  p.Port,
				InstancePort: p.Port,
			})
		}
	}

	// Get the GRPC port.
	var grpcPort uint16
	for _, p := range cfg.Ports {
		if p.Protocol == model.ProtocolGRPC {
			grpcPort = uint16(p.InstancePort)
			break
		}
	}

	// Create the client for sending forward requests.
	if grpcPort == 0 {
		// This should never happen, since we're manually adding a GRPC port if one doesn't exist.
		return nil, errors.New("unable to find grpc port for application")
	}

	out.Instance, err = client.New(fmt.Sprintf("%s:%d", localhost, grpcPort))
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (w *workload) Address() string {
	return localhost
}

func (w *workload) Sidecar() echo.Sidecar {
	return w.sidecar
}

func (w *workload) Call(opts *echo.CallOptions) (client.ParsedResponses, error) {
	// Override the Host.
	opts.Host = localhost

	portSelector := common.IdentityOutboundPortSelector
	if w.discoveryFilter != nil {
		// Use the discovery filter as the outbound port selector to force outbound
		// requests to go through Envoy.
		portSelector = w.discoveryFilter.GetBoundOutboundListenerPort
	}

	return common.CallEcho(w.Instance, opts, portSelector)
}

func (w *workload) Close() (err error) {
	if w.Instance != nil {
		err = multierror.Append(err, w.Instance.Close()).ErrorOrNil()
	}
	if w.sidecar != nil {
		w.sidecar.Stop()
	}
	if w.discoveryFilter != nil {
		w.discoveryFilter.Stop()
	}
	if w.echoServer != nil {
		err = multierror.Append(err, w.echoServer.Close()).ErrorOrNil()
	}
	return
}
