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

package istio

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	envoyconfigcorev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	envoyservicediscoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kube2 "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
)

// Discovery allows interacting with the discovery server in a cluster to check for configuration correctness.
type Discovery interface {
	CallDiscovery(req *envoyservicediscoveryv3.DiscoveryRequest) (*envoyservicediscoveryv3.DiscoveryResponse, error)
	CallDiscoveryOrFail(t test.Failer, req *envoyservicediscoveryv3.DiscoveryRequest) *envoyservicediscoveryv3.DiscoveryResponse

	StartDiscovery(req *envoyservicediscoveryv3.DiscoveryRequest) error
	StartDiscoveryOrFail(t test.Failer, req *envoyservicediscoveryv3.DiscoveryRequest)
	WatchDiscovery(duration time.Duration, accept func(*envoyservicediscoveryv3.DiscoveryResponse) (bool, error)) error
	WatchDiscoveryOrFail(t test.Failer, duration time.Duration, accept func(*envoyservicediscoveryv3.DiscoveryResponse) (bool, error))
}

// Discovery returns the service discovery client for the given cluster or the first configured
// cluster in the environment if given nil.
func (i *operatorComponent) Discovery(cluster resource.Cluster) (Discovery, error) {
	// lazy init since it requires some k8s calls, port-forwarding and building grpc clients
	if len(i.discovery) == 0 {
		if err := i.initDiscovery(); err != nil {
			return nil, err
		}
	}

	if d := i.discovery[cluster.Index()]; d != nil {
		return d, nil
	}

	return nil, fmt.Errorf("no service discovery client for %s", cluster.Name())
}

// DiscoveryOrFail returns the service discovery client for the given cluster or the first configured
// cluster in the environment if given nil. The test will be failed if there is an error.
func (i *operatorComponent) DiscoveryOrFail(f test.Failer, cluster resource.Cluster) Discovery {
	d, err := i.Discovery(cluster)
	if err != nil {
		f.Fatal(err)
	}
	return d
}

func (i *operatorComponent) initDiscovery() error {
	env := i.ctx.Environment()
	i.discovery = make([]Discovery, len(i.ctx.Clusters()))
	for _, c := range env.Clusters() {
		cp, err := env.GetControlPlaneCluster(c)
		if err != nil {
			return err
		}
		if inst := i.discovery[cp.Index()]; inst == nil {
			i.discovery[cp.Index()], err = i.newDiscovery(cp)
			if err != nil {
				return fmt.Errorf("error creating discovery instance for cluster %d: %v", cp.Index(), err)
			}
		}
		i.discovery[c.Index()] = i.discovery[cp.Index()]
	}
	return nil
}

// NewDiscoveryRequest is a utility method for creating a new request for the given node and type.
func NewDiscoveryRequest(nodeID string, typeURL string) *envoyservicediscoveryv3.DiscoveryRequest {
	return &envoyservicediscoveryv3.DiscoveryRequest{
		Node: &envoyconfigcorev3.Node{
			Id: nodeID,
		},
		TypeUrl: typeURL,
	}
}

const (
	discoveryService = "istiod"
	grpcPortName     = "grpc-xds"
)

var (
	_ Discovery = &discoveryImpl{}
)

func (i *operatorComponent) newDiscovery(cluster resource.Cluster) (Discovery, error) {
	c := &discoveryImpl{
		cluster: i.ctx.Clusters().GetOrDefault(cluster),
	}

	ns := i.settings.SystemNamespace

	fetchFn := kube.NewSinglePodFetch(c.cluster, ns, "istio=discovery")
	pods, err := kube.WaitUntilPodsAreReady(fetchFn)
	if err != nil {
		return nil, err
	}
	pod := pods[0]

	port, err := c.getGrpcPort(ns)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			_ = c.close()
		}
	}()

	// Start port-forwarding for discovery.
	c.forwarder, err = c.cluster.NewPortForwarder(pod.Name, pod.Namespace, "", 0, int(port))
	if err != nil {
		return nil, err
	}
	if err = c.forwarder.Start(); err != nil {
		return nil, err
	}

	var addr *net.TCPAddr
	addr, err = net.ResolveTCPAddr("tcp", c.forwarder.Address())
	if err != nil {
		return nil, err
	}

	c.discoveryClient, err = newClient(addr)
	if err != nil {
		return nil, err
	}

	return c, nil
}

type discoveryImpl struct {
	*discoveryClient

	forwarder kube2.PortForwarder

	cluster resource.Cluster
}

func (c *discoveryImpl) close() (err error) {
	if c.discoveryClient != nil {
		err = multierror.Append(err, c.discoveryClient.close()).ErrorOrNil()
		c.discoveryClient = nil
	}

	if c.forwarder != nil {
		c.forwarder.Close()
		c.forwarder = nil
	}
	return
}

func (c *discoveryImpl) getGrpcPort(ns string) (uint16, error) {
	svc, err := c.cluster.CoreV1().Services(ns).Get(context.TODO(), discoveryService, v1.GetOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve service %s: %v", discoveryService, err)
	}
	for _, portInfo := range svc.Spec.Ports {
		if portInfo.Name == grpcPortName {
			return uint16(portInfo.TargetPort.IntValue()), nil
		}
	}
	return 0, fmt.Errorf("failed to get target port in service %s", discoveryService)
}

type discoveryClient struct {
	discoveryAddr *net.TCPAddr
	conn          *grpc.ClientConn
	stream        envoyservicediscoveryv3.AggregatedDiscoveryService_StreamAggregatedResourcesClient
	lastRequest   *envoyservicediscoveryv3.DiscoveryRequest

	wg sync.WaitGroup
}

func newClient(discoveryAddr *net.TCPAddr) (*discoveryClient, error) {
	conn, err := grpc.Dial(discoveryAddr.String(), grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	adsClient := envoyservicediscoveryv3.NewAggregatedDiscoveryServiceClient(conn)
	stream, err := adsClient.StreamAggregatedResources(context.Background())
	if err != nil {
		return nil, err
	}

	return &discoveryClient{
		conn:          conn,
		stream:        stream,
		discoveryAddr: discoveryAddr,
	}, nil
}

func (c *discoveryClient) CallDiscovery(req *envoyservicediscoveryv3.DiscoveryRequest) (*envoyservicediscoveryv3.DiscoveryResponse, error) {
	c.lastRequest = req
	err := c.stream.Send(req)
	if err != nil {
		return nil, err
	}
	return c.stream.Recv()
}

func (c *discoveryClient) CallDiscoveryOrFail(t test.Failer, req *envoyservicediscoveryv3.DiscoveryRequest) *envoyservicediscoveryv3.DiscoveryResponse {
	t.Helper()
	resp, err := c.CallDiscovery(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (c *discoveryClient) StartDiscovery(req *envoyservicediscoveryv3.DiscoveryRequest) error {
	c.lastRequest = req
	err := c.stream.Send(req)
	if err != nil {
		return err
	}
	return nil
}

func (c *discoveryClient) StartDiscoveryOrFail(t test.Failer, req *envoyservicediscoveryv3.DiscoveryRequest) {
	t.Helper()
	if err := c.StartDiscovery(req); err != nil {
		t.Fatal(err)
	}
}

func (c *discoveryClient) WatchDiscovery(timeout time.Duration,
	accept func(*envoyservicediscoveryv3.DiscoveryResponse) (bool, error)) error {
	c1 := make(chan error, 1)

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		for {
			result, err := c.stream.Recv()
			if err != nil {
				c1 <- err
				break
			}
			// ACK all responses so that when an update arrives we can receive it
			err = c.stream.Send(&envoyservicediscoveryv3.DiscoveryRequest{
				Node:          c.lastRequest.Node,
				ResponseNonce: result.Nonce,
				VersionInfo:   result.VersionInfo,
				TypeUrl:       c.lastRequest.TypeUrl,
				ResourceNames: c.lastRequest.ResourceNames,
			})
			if err != nil {
				c1 <- err
				break
			}
			accepted, err := accept(result)
			if err != nil {
				c1 <- err
				break
			}
			if accepted {
				c1 <- nil
				break
			}
		}
	}()
	select {
	case err := <-c1:
		return err
	case <-time.After(timeout):
		return errors.New("timed out")
	}
}

func (c *discoveryClient) WatchDiscoveryOrFail(t test.Failer, timeout time.Duration,
	accept func(*envoyservicediscoveryv3.DiscoveryResponse) (bool, error)) {

	t.Helper()
	if err := c.WatchDiscovery(timeout, accept); err != nil {
		t.Fatalf("no resource accepted: %v", err)
	}
}

func (c *discoveryClient) close() (err error) {
	if c.stream != nil {
		err = multierror.Append(err, c.stream.CloseSend()).ErrorOrNil()
	}
	if c.conn != nil {
		err = multierror.Append(err, c.conn.Close()).ErrorOrNil()
	}

	c.wg.Wait()

	return
}
