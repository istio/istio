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

package native

import (
	"io"
	"sync"

	"github.com/docker/docker/client"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test/docker"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/api"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/reserveport"
)

const (
	systemNamespace = "istio-system"
	domain          = "cluster.local"

	networkLabelKey   = "app"
	networkLabelValue = "istio-test"
)

var _ io.Closer = &Environment{}

// Environment for testing natively on the host machine. It implements api.Environment, and also
// hosts publicly accessible methods that are specific to local environment.
type Environment struct {
	id  resource.ID
	ctx api.Context

	// SystemNamespace is the namespace used for all Istio system components.
	SystemNamespace string

	// Domain used by components in the native environment.
	Domain string

	// PortManager provides free ports on-demand.
	PortManager reserveport.PortManager

	// Docker resources, Lazy-initialized.
	dockerClient *client.Client
	network      *docker.Network
	mux          sync.Mutex
}

var _ resource.Environment = &Environment{}

// New returns a new native environment.
func New(ctx api.Context) (resource.Environment, error) {
	portMgr, err := reserveport.NewPortManager()
	if err != nil {
		return nil, err
	}

	e := &Environment{
		ctx:             ctx,
		SystemNamespace: systemNamespace,
		Domain:          domain,
		PortManager:     portMgr,
	}
	e.id = ctx.TrackResource(e)

	// Set the trust domain.
	spiffe.SetTrustDomain(domain)

	return e, nil
}

// EnvironmentName implements environment.Instance
func (e *Environment) EnvironmentName() environment.Name {
	return environment.Native
}

// Case implements environment.Instance
func (e *Environment) Case(name environment.Name, fn func()) {
	if name == e.EnvironmentName() {
		fn()
	}
}

// ID implements resource.Instance
func (e *Environment) ID() resource.ID {
	return e.id
}

func (e *Environment) DockerClient() (*client.Client, error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	if e.dockerClient == nil {
		// Create a shared network for Docker containers.
		c, err := client.NewClientWithOpts(client.FromEnv)
		if err != nil {
			return nil, err
		}
		e.dockerClient = c
	}

	return e.dockerClient, nil
}

func (e *Environment) Network() (*docker.Network, error) {
	c, err := e.DockerClient()
	if err != nil {
		return nil, err
	}

	e.mux.Lock()
	defer e.mux.Unlock()

	if e.network == nil {
		networkName := "istio-" + e.ctx.Settings().RunID.String()
		n, err := docker.NewNetwork(c, docker.NetworkConfig{
			Name: networkName,
			Labels: map[string]string{
				networkLabelKey: networkLabelValue,
			},
		})
		if err != nil {
			return nil, err
		}

		// Save the network.
		e.network = n
	}

	return e.network, nil
}

func (e *Environment) Close() (err error) {
	e.mux.Lock()
	defer e.mux.Unlock()

	if e.PortManager != nil {
		err = multierror.Append(err, e.PortManager.Close()).ErrorOrNil()
	}
	e.PortManager = nil

	if e.network != nil {
		err = multierror.Append(err, e.network.Close()).ErrorOrNil()
	}
	e.network = nil

	if e.dockerClient != nil {
		err = multierror.Append(err, e.dockerClient.Close()).ErrorOrNil()
	}
	e.dockerClient = nil
	return
}
