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
	"net"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	adsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"

	"fmt"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/environments/local"
	"istio.io/istio/pkg/test/kube"
)

const (
	pilotAdsPort = 15010
	localhost    = "127.0.0.1"
)

type deployedPilot struct {
	client *pilotClient

	// Only used for k8s.
	forwarder *kube.PortForwarder

	// Only used for running Pilot locally.
	model.ConfigStoreCache
	server   *bootstrap.Server
	stopChan chan struct{}
}

// InitLocal initializes a new pilot instance for the local environment
func InitLocal(ctx environment.ComponentContext) (interface{}, error) {
	e, ok := ctx.Environment().(*local.Implementation)
	if !ok {
		return nil, fmt.Errorf("expected environment not found")
	}

	return NewLocalPilot(e.IstioSystemNamespace)
}

// InitKube initializes a new Pilot component for the kubernetes environment.
func InitKube(ctx environment.ComponentContext) (interface{}, error) {
	e, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("expected environment not found")
	}

	pod, err := e.Accessor.WaitForPodBySelectors(e.IstioSystemNamespace, "istio=pilot")
	if err != nil {
		return nil, err
	}

	return NewKubePilot(ctx.Settings().KubeConfig, pod.Namespace, pod.Name)
}

// NewLocalPilot creates a new pilot for the local environment.
func NewLocalPilot(namespace string) (environment.DeployedPilot, error) {
	// Use an in-memory config store.
	configController := memory.NewController(memory.Make(model.IstioConfigTypes))

	mesh := model.DefaultMeshConfig()
	options := envoy.DiscoveryServiceOptions{
		HTTPAddr:       ":0",
		MonitoringAddr: ":0",
		GrpcAddr:       ":0",
		SecureGrpcAddr: ":0",
	}
	bootstrapArgs := bootstrap.PilotArgs{
		Namespace:        namespace,
		DiscoveryOptions: options,
		MeshConfig:       &mesh,
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

	client, err := newPilotClient(server.GRPCListeningAddr.(*net.TCPAddr))
	if err != nil {
		return nil, err
	}

	// Start the server
	stopChan := make(chan struct{})
	_, err = server.Start(stopChan)
	if err != nil {
		return nil, err
	}

	return &deployedPilot{
		ConfigStoreCache: configController,
		server:           server,
		client:           client,
		stopChan:         stopChan,
	}, nil
}

// NewKubePilot creates a new pilot instance for the kubernetes environment
func NewKubePilot(kubeConfig, namespace, pod string) (environment.DeployedPilot, error) {
	// Start port-forwarding for pilot.
	// TODO(nmittler): Don't use a hard-coded port.
	forwarder := kube.NewPortForwarder(kubeConfig, namespace, pod, pilotAdsPort)
	if err := forwarder.Start(); err != nil {
		return nil, err
	}

	addr, err := net.ResolveTCPAddr(localhost, forwarder.Address())
	if err != nil {
		return nil, err
	}
	client, err := newPilotClient(addr)
	if err != nil {
		return nil, err
	}

	return &deployedPilot{
		client:    client,
		forwarder: forwarder,
	}, nil
}

// CallDiscovery implements the DeployedPilot interface.
func (p *deployedPilot) CallDiscovery(req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	return p.client.callDiscovery(req)
}

// Close stops the pilot server.
func (p *deployedPilot) Close() (err error) {
	if p.client != nil {
		err = multierror.Append(err, p.client.Close()).ErrorOrNil()
	}

	if p.stopChan != nil {
		p.stopChan <- struct{}{}
	}

	if p.forwarder != nil {
		p.forwarder.Close()
	}

	return
}

type pilotClient struct {
	discoveryAddr *net.TCPAddr
	conn          *grpc.ClientConn
	stream        adsapi.AggregatedDiscoveryService_StreamAggregatedResourcesClient
}

func newPilotClient(discoveryAddr *net.TCPAddr) (*pilotClient, error) {
	conn, err := grpc.Dial(discoveryAddr.String(), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	adsClient := adsapi.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := adsClient.StreamAggregatedResources(context.Background())
	if err != nil {
		return nil, err
	}

	return &pilotClient{
		conn:          conn,
		stream:        stream,
		discoveryAddr: discoveryAddr,
	}, nil
}

func (c *pilotClient) callDiscovery(req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
	err := c.stream.Send(req)
	if err != nil {
		return nil, err
	}
	return c.stream.Recv()
}

func (c *pilotClient) Close() (err error) {
	if c.stream != nil {
		err = multierror.Append(err, c.stream.CloseSend()).ErrorOrNil()
	}
	if c.conn != nil {
		err = multierror.Append(err, c.conn.Close()).ErrorOrNil()
	}
	return
}
