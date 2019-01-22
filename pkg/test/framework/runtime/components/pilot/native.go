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
	"io"
	"net"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pkg/keepalive"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/native"
)

var _ Native = &nativeComponent{}
var _ io.Closer = &nativeComponent{}

// TODO(nmittler): Revisit the need for this interface after galley is the config solution for the framework.

// Native is the interface for an native pilot server.
type Native interface {
	components.Pilot
	model.ConfigStore
	GetDiscoveryAddress() *net.TCPAddr
}

// NewNativeComponent factory function for the component
func NewNativeComponent() (api.Component, error) {
	return &nativeComponent{
		stopChan: make(chan struct{}),
	}, nil
}

type nativeComponent struct {
	*client
	model.ConfigStoreCache
	server   *bootstrap.Server
	stopChan chan struct{}
	scope    lifecycle.Scope
}

func (c *nativeComponent) Descriptor() component.Descriptor {
	return descriptors.Pilot
}

func (c *nativeComponent) Scope() lifecycle.Scope {
	return c.scope
}

func (c *nativeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	env, err := native.GetEnvironment(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()

	// Dynamically assign all ports.
	options := envoy.DiscoveryServiceOptions{
		HTTPAddr:       ":0",
		MonitoringAddr: ":0",
		GrpcAddr:       ":0",
		SecureGrpcAddr: "",
	}

	bootstrapArgs := bootstrap.PilotArgs{
		Namespace:        env.Namespace,
		DiscoveryOptions: options,
		MeshConfig:       env.Mesh,
		Config: bootstrap.ConfigArgs{
			Controller: env.ServiceManager.ConfigStore,
		},
		// Use the config store for service entries as well.
		Service: bootstrap.ServiceArgs{
			// A ServiceEntry registry is added by default, which is what we want. Don't include any other registries.
			Registries: []string{},
		},
		// Include all of the default plugins for integration with Mixer, etc.
		Plugins:   bootstrap.DefaultPlugins,
		ForceStop: true,
	}

	// Save the config store.
	c.ConfigStoreCache = env.ServiceManager.ConfigStore

	// Create the server for the discovery service.
	c.server, err = bootstrap.NewServer(bootstrapArgs)
	if err != nil {
		return err
	}

	c.client, err = newClient(c.server.GRPCListeningAddr.(*net.TCPAddr))
	if err != nil {
		return err
	}

	// Start the server
	err = c.server.Start(c.stopChan)
	return
}

func (c *nativeComponent) Close() (err error) {
	if c.client != nil {
		err = multierror.Append(err, c.client.Close()).ErrorOrNil()
	}

	if c.stopChan != nil {
		c.stopChan <- struct{}{}
	}
	return
}

// GetDiscoveryAddress gets the discovery address for pilot.
func (c *nativeComponent) GetDiscoveryAddress() *net.TCPAddr {
	return c.server.GRPCListeningAddr.(*net.TCPAddr)
}
