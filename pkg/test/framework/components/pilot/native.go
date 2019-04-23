//  Copyright 2019 Istio Authors
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
	"errors"
	"io"
	"net"

	"github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/resource"
)

var _ Instance = &nativeComponent{}
var _ io.Closer = &nativeComponent{}
var _ Native = &nativeComponent{}

// Native is the interface for an native pilot server.
type Native interface {
	Instance
	GetDiscoveryAddress() *net.TCPAddr
}

type nativeComponent struct {
	id resource.ID

	environment *native.Environment
	*client
	server   *bootstrap.Server
	stopChan chan struct{}
	config   Config
}

// NewNativeComponent factory function for the component
func newNative(ctx resource.Context, config Config) (Instance, error) {
	if config.Galley == nil {
		return nil, errors.New("galley must be provided")
	}

	env := ctx.Environment().(*native.Environment)
	instance := &nativeComponent{
		environment: ctx.Environment().(*native.Environment),
		stopChan:    make(chan struct{}),
		config:      config,
	}
	instance.id = ctx.TrackResource(instance)

	// Dynamically assign all ports.
	options := envoy.DiscoveryServiceOptions{
		HTTPAddr:       ":0",
		MonitoringAddr: ":0",
		GrpcAddr:       ":0",
		SecureGrpcAddr: "",
	}

	bootstrapArgs := bootstrap.PilotArgs{
		Namespace:        env.SystemNamespace,
		DiscoveryOptions: options,
		MeshConfig:       instance.environment.Mesh,
		// Use the config store for service entries as well.
		Service: bootstrap.ServiceArgs{
			// A ServiceEntry registry is added by default, which is what we want. Don't include any other registries.
			Registries: []string{},
		},
		// Include all of the default plugins for integration with Mixer, etc.
		Plugins:   bootstrap.DefaultPlugins,
		ForceStop: true,
	}

	if bootstrapArgs.MeshConfig == nil {
		bootstrapArgs.MeshConfig = &meshconfig.MeshConfig{}
	}
	// Set as MCP address, note needs to strip 'tcp://' from the address prefix
	bootstrapArgs.MeshConfig.ConfigSources = []*meshconfig.ConfigSource{
		{Address: config.Galley.Address()[6:]},
	}
	bootstrapArgs.MCPMaxMessageSize = bootstrap.DefaultMCPMaxMsgSize

	var err error
	// Create the server for the discovery service.
	if instance.server, err = bootstrap.NewServer(bootstrapArgs); err != nil {
		return nil, err
	}

	if instance.client, err = newClient(instance.server.GRPCListeningAddr.(*net.TCPAddr)); err != nil {
		return nil, err
	}

	// Start the server
	if err = instance.server.Start(instance.stopChan); err != nil {
		return nil, err
	}

	return instance, nil
}

// ID implements resource.Instance
func (c *nativeComponent) ID() resource.ID {
	return c.id
}

func (c *nativeComponent) Close() (err error) {
	if c.client != nil {
		err = multierror.Append(err, c.client.Close()).ErrorOrNil()
	}

	if c.stopChan != nil {
		close(c.stopChan)
	}
	return
}

// GetDiscoveryAddress gets the discovery address for pilot.
func (c *nativeComponent) GetDiscoveryAddress() *net.TCPAddr {
	return c.server.GRPCListeningAddr.(*net.TCPAddr)
}
