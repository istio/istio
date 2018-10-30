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

package pilot

import (
	"fmt"
	"net"

	"istio.io/istio/pkg/test/framework/scopes"

	meshConfig "istio.io/api/mesh/v1alpha1"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/local"
)

var (
	// LocalComponent is a component for the local environment.
	LocalComponent = &localComponent{}
)

// NewLocalPilot creates a new pilot for the local environment.
func NewLocalPilot(namespace string, mesh *meshConfig.MeshConfig, configStore model.ConfigStoreCache) (out LocalPilot, err error) {
	p := &localPilot{
		ConfigStoreCache: configStore,
	}
	scopes.CI.Info("=== BEGIN: Starting local Pilot ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Start local Pilot ===")
			_ = p.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Start local Pilot ===")
		}
	}()

	options := envoy.DiscoveryServiceOptions{
		HTTPAddr:       ":0",
		MonitoringAddr: ":0",
		GrpcAddr:       ":0",
		SecureGrpcAddr: ":0",
	}
	bootstrapArgs := bootstrap.PilotArgs{
		Namespace:        namespace,
		DiscoveryOptions: options,
		MeshConfig:       mesh,
		Config: bootstrap.ConfigArgs{
			Controller: configStore,
		},
		// Use the config store for service entries as well.
		Service: bootstrap.ServiceArgs{
			// A ServiceEntry registry is added by default, which is what we want. Don't include any other registries.
			Registries: []string{},
		},
		// Include all of the default plugins for integration with Mixer, etc.
		Plugins: bootstrap.DefaultPlugins,
	}

	// Create the server for the discovery service.
	p.server, err = bootstrap.NewServer(bootstrapArgs)
	if err != nil {
		return nil, err
	}
	// Start the server
	p.stopChan = make(chan struct{})
	if err = p.server.Start(p.stopChan); err != nil {
		return nil, err
	}

	p.pilotClient, err = newPilotClient(p.server.GRPCListeningAddr.(*net.TCPAddr))
	if err != nil {
		return nil, err
	}

	return p, nil
}

type localComponent struct{}

// ID implements the component.Component interface.
func (c *localComponent) ID() dependency.Instance {
	return dependency.Pilot
}

// Requires implements the component.Component interface.
func (c *localComponent) Requires() []dependency.Instance {
	return requiredDeps
}

// Init implements the component.Component interface.
func (c *localComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	e, ok := ctx.Environment().(*local.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	return NewLocalPilot(e.IstioSystemNamespace, e.Mesh, e.ServiceManager.ConfigStore)
}

// LocalPilot is the interface for a local pilot server.
type LocalPilot interface {
	environment.DeployedPilot
	model.ConfigStore
	GetDiscoveryAddress() *net.TCPAddr
}

type localPilot struct {
	*pilotClient
	model.ConfigStoreCache
	server   *bootstrap.Server
	stopChan chan struct{}
}

// GetDiscoveryAddress gets the discovery address for pilot.
func (p *localPilot) GetDiscoveryAddress() *net.TCPAddr {
	return p.server.GRPCListeningAddr.(*net.TCPAddr)
}

// Close stops the local pilot server.
func (p *localPilot) Close() (err error) {
	if p.pilotClient != nil {
		err = multierror.Append(err, p.pilotClient.Close()).ErrorOrNil()
	}

	if p.stopChan != nil {
		p.stopChan <- struct{}{}
	}
	return
}
