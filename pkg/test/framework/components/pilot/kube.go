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
	"fmt"
	"net"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	adsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/kube"
)

const (
	pilotService = "istio-pilot"
	grpcPortName = "grpc-xds"
)

var (
	// KubeComponent is a component for the Kubernetes environment.
	KubeComponent = &kubeComponent{}
)

// NewKubePilot creates a new pilot instance for the kubernetes environment
func NewKubePilot(accessor *kube.Accessor, namespace, pod string, port uint16) (out environment.DeployedPilot, err error) {
	p := &kubePilot{}
	defer func() {
		if err != nil {
			_ = p.Close()
		}
	}()

	// Start port-forwarding for pilot.
	options := &kube.PodSelectOptions{
		PodNamespace: namespace,
		PodName:      pod,
	}
	var forwarder kube.PortForwarder
	forwarder, err = accessor.NewPortForwarder(options, 0, port)
	if err != nil {
		return nil, err
	}
	if err = forwarder.Start(); err != nil {
		return nil, err
	}
	p.forwarder = forwarder

	var addr *net.TCPAddr
	addr, err = net.ResolveTCPAddr("tcp", forwarder.Address())
	if err != nil {
		return nil, err
	}

	var client *pilotClient
	client, err = newPilotClient(addr)
	if err != nil {
		return nil, err
	}
	p.pilotClient = client

	return p, nil
}

type kubeComponent struct {
}

// ID implements the component.Component interface.
func (c *kubeComponent) ID() dependency.Instance {
	return dependency.Pilot
}

// Requires implements the component.Component interface.
func (c *kubeComponent) Requires() []dependency.Instance {
	return requiredDeps
}

// Init implements the component.Component interface.
func (c *kubeComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	e, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	result, err := c.doInit(e)
	if err != nil {
		return nil, multierror.Prefix(err, "pilot init failed:")
	}
	return result, nil
}

func (c *kubeComponent) doInit(e *kubernetes.Implementation) (interface{}, error) {
	s := e.KubeSettings()

	fetchFn := e.Accessor.NewSinglePodFetch(s.IstioSystemNamespace, "istio=pilot")
	if err := e.Accessor.WaitUntilPodsAreReady(fetchFn); err != nil {
		return nil, err
	}
	pods, err := fetchFn()
	if err != nil {
		return nil, err
	}
	pod := pods[0]

	port, err := getGrpcPort(e)
	if err != nil {
		return nil, err
	}

	return NewKubePilot(e.Accessor, pod.Namespace, pod.Name, port)
}

func getGrpcPort(e *kubernetes.Implementation) (uint16, error) {
	s := e.KubeSettings()
	svc, err := e.Accessor.GetService(s.IstioSystemNamespace, pilotService)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve service %s: %v", pilotService, err)
	}
	for _, portInfo := range svc.Spec.Ports {
		if portInfo.Name == grpcPortName {
			return uint16(portInfo.TargetPort.IntValue()), nil
		}
	}
	return 0, fmt.Errorf("failed to get target port in service %s", pilotService)
}

type kubePilot struct {
	*pilotClient
	forwarder kube.PortForwarder
}

// Close stops the kube pilot server.
func (p *kubePilot) Close() (err error) {
	if p.pilotClient != nil {
		err = multierror.Append(err, p.pilotClient.Close()).ErrorOrNil()
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

func (c *pilotClient) CallDiscovery(req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error) {
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
