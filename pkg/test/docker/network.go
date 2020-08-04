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

package docker

import (
	"context"
	"io"
	"net"

	"istio.io/istio/pkg/test/scopes"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

var _ io.Closer = &Network{}

type NetworkConfig struct {
	Name   string
	Labels map[string]string
}

// Network is an instance of a user-defined Docker network.
type Network struct {
	NetworkConfig

	Subnet *net.IPNet

	id           string
	dockerClient *client.Client
}

// NewNetwork creates a new user-defined Docker network.
func NewNetwork(dockerClient *client.Client, cfg NetworkConfig) (out *Network, err error) {
	scopes.Framework.Infof("Creating Docker network %s", cfg.Name)
	resp, err := dockerClient.NetworkCreate(context.Background(), cfg.Name, types.NetworkCreate{
		CheckDuplicate: true,
		Labels:         cfg.Labels,
	})
	if err != nil {
		return nil, err
	}

	scopes.Framework.Infof("Docker network %s created (ID=%s)", cfg.Name, resp.ID)

	n := &Network{
		NetworkConfig: cfg,
		dockerClient:  dockerClient,
		id:            resp.ID,
	}
	defer func() {
		if err != nil {
			_ = n.Close()
		}
	}()

	// Retrieve the subnet for the network.
	iresp, err := dockerClient.NetworkInspect(context.Background(), resp.ID, types.NetworkInspectOptions{})
	if err != nil {
		return nil, err
	}
	if _, n.Subnet, err = net.ParseCIDR(iresp.IPAM.Config[0].Subnet); err != nil {
		return nil, err
	}

	return n, nil
}

// Close removes this network. All attached containers must already have been stopped.
func (n *Network) Close() error {
	scopes.Framework.Infof("Closing Docker network %s (ID=%s)", n.Name, n.id)
	return n.dockerClient.NetworkRemove(context.Background(), n.id)
}
