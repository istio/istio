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
	"context"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	adsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/test/environment"
)

type deployedPilot struct {
	model.ConfigStoreCache
	server *bootstrap.Server
	client *pilotClient
	stop   chan struct{}
}

// NewPilot creates a new local pilot instance.
func NewPilot(args Args) (environment.DeployedPilot, error) {
	// Use an in-memory config store.
	configController := memory.NewController(memory.Make(model.IstioConfigTypes))

	bootstrapArgs := bootstrap.PilotArgs{
		Namespace:        args.Namespace,
		DiscoveryOptions: args.Options,
		MeshConfig:       args.Mesh,
		Config: bootstrap.ConfigArgs{
			Controller: configController,
		},
		// Use the config store for service entries as well.
		Service: bootstrap.ServiceArgs{
			Registries: []string{
				string(serviceregistry.ConfigRegistry),
			},
		},
	}

	// Create the server for the discovery service.
	server, err := bootstrap.NewServer(bootstrapArgs)
	if err != nil {
		return nil, err
	}

	client, err := newPilotClient(server.GRPCListeningAddr.String())
	if err != nil {
		return nil, err
	}

	// Start the server
	stop := make(chan struct{})
	_, err = server.Start(stop)
	if err != nil {
		return nil, err
	}

	return &deployedPilot{
		ConfigStoreCache: configController,
		server:           server,
		client:           client,
		stop:             stop,
	}, nil
}

// CallDiscovery implements the DeployedPilot interface.
func (p *deployedPilot) CallDiscovery(req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	return p.client.callDiscovery(req)
}

// Stop stops the pilot server. TODO(nmittler): Need to make this part of the API.
func (p *deployedPilot) Stop() {
	p.client.close()
	p.stop <- struct{}{}
}

type pilotClient struct {
	conn   *grpc.ClientConn
	stream adsapi.AggregatedDiscoveryService_StreamAggregatedResourcesClient
}

func newPilotClient(grpcAddr string) (*pilotClient, error) {
	conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	adsClient := adsapi.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := adsClient.StreamAggregatedResources(context.Background())
	if err != nil {
		return nil, err
	}

	return &pilotClient{
		conn:   conn,
		stream: stream,
	}, nil
}

func (c *pilotClient) callDiscovery(req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	err := c.stream.Send(req)
	if err != nil {
		return nil, err
	}
	return c.stream.Recv()
}

func (c *pilotClient) close() {
	_ = c.stream.CloseSend()
	_ = c.conn.Close()
}
