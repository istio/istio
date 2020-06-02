//  Copyright Istio Authors
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
	"os"
	"path"
	"time"

	"github.com/hashicorp/go-multierror"

	meshapi "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var _ Instance = &nativeComponent{}
var _ io.Closer = &nativeComponent{}
var _ Native = &nativeComponent{}

// Native is the interface for an native pilot server.
type Native interface {
	Instance
	GetDiscoveryAddress() *net.TCPAddr
	GetSecureDiscoveryAddress() *net.TCPAddr
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
func newNative(ctx resource.Context, cfg Config) (Instance, error) {
	e := ctx.Environment().(*native.Environment)
	if cfg.Cluster == nil {
		cfg.Cluster = e.Cluster
	}
	instance := &nativeComponent{
		environment: ctx.Environment().(*native.Environment),
		stopChan:    make(chan struct{}),
		config:      cfg,
	}
	instance.id = ctx.TrackResource(instance)

	// Dynamically assign all ports.
	options := bootstrap.DiscoveryServiceOptions{
		HTTPAddr:       ":0",
		MonitoringAddr: ":0",
		GrpcAddr:       ":0",
	}

	tmpMesh := mesh.DefaultMeshConfig()
	m := &tmpMesh
	if cfg.MeshConfig != nil {
		m = cfg.MeshConfig
	}
	m.AccessLogFile = "./var/log/istio/access.log"

	if cfg.ServiceArgs.Registries == nil {
		cfg.ServiceArgs = bootstrap.ServiceArgs{
			// A ServiceEntry registry is added by default, which is what we want. Don't include any other registries.
			Registries: []string{},
		}
	}

	bootstrapArgs := bootstrap.NewPilotArgs(func(p *bootstrap.PilotArgs) {
		p.Namespace = e.SystemNamespace
		p.DiscoveryOptions = options
		p.Config = bootstrap.ConfigArgs{
			ControllerOptions: controller.Options{
				DomainSuffix: e.Domain,
			},
		}
		p.MeshConfig = m

		// Use the config store for service entries as well.
		p.Service = cfg.ServiceArgs

		// Include all of the default plugins for integration with Mixer, etc.
		p.Plugins = bootstrap.DefaultPlugins
		p.ShutdownDuration = 1 * time.Millisecond
	})

	if bootstrapArgs.MeshConfig == nil {
		bootstrapArgs.MeshConfig = &meshapi.MeshConfig{}
	}
	bootstrapArgs.Config.FileDir = cfg.Cluster.(native.Cluster).GetConfigDir()

	// Use testing certs
	if err := os.Setenv(bootstrap.LocalCertDir.Name, path.Join(env.IstioSrc, "tests/testdata/certs/pilot")); err != nil {
		return nil, err
	}
	var err error
	// Create the server for the discovery service.
	if instance.server, err = bootstrap.NewServer(bootstrapArgs); err != nil {
		return nil, err
	}

	// Start the server
	if err = instance.server.Start(instance.stopChan); err != nil {
		return nil, err
	}

	time.Sleep(1 * time.Second)
	if instance.client, err = newClient(instance.server.GRPCListener.Addr().(*net.TCPAddr)); err != nil {
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
		scopes.Framework.Debugf("%s closing client", c.id)
		err = multierror.Append(err, c.client.Close()).ErrorOrNil()
	}

	if c.stopChan != nil {
		scopes.Framework.Debugf("%s stopping Pilot server", c.id)
		close(c.stopChan)
	}

	scopes.Framework.Debugf("%s close complete (err:%v)", c.id, err)
	return
}

// GetDiscoveryAddress gets the discovery address for pilot.
func (c *nativeComponent) GetDiscoveryAddress() *net.TCPAddr {
	return c.server.GRPCListener.Addr().(*net.TCPAddr)
}

// GetSecureDiscoveryAddress gets the discovery address for pilot.
func (c *nativeComponent) GetSecureDiscoveryAddress() *net.TCPAddr {
	return c.server.SecureGrpcListener.Addr().(*net.TCPAddr)
}
