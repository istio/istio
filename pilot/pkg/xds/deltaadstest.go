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

package xds

import (
	"context"
	"sync"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test"
)

func NewDeltaAdsTest(t test.Failer, conn *grpc.ClientConn) *DeltaAdsTest {
	test.SetForTest(t, &features.DeltaXds, true)
	return NewDeltaXdsTest(t, conn, func(conn *grpc.ClientConn) (DeltaDiscoveryClient, error) {
		xds := discovery.NewAggregatedDiscoveryServiceClient(conn)
		return xds.DeltaAggregatedResources(context.Background())
	})
}

func NewDeltaXdsTest(t test.Failer, conn *grpc.ClientConn,
	getClient func(conn *grpc.ClientConn) (DeltaDiscoveryClient, error),
) *DeltaAdsTest {
	ctx, cancel := context.WithCancel(context.Background())

	cl, err := getClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	resp := &DeltaAdsTest{
		client:        cl,
		conn:          conn,
		context:       ctx,
		cancelContext: cancel,
		t:             t,
		ID:            "sidecar~1.1.1.1~test.default~default.svc.cluster.local",
		timeout:       time.Second,
		Type:          v3.ClusterType,
		responses:     make(chan *discovery.DeltaDiscoveryResponse),
		error:         make(chan error),
	}
	t.Cleanup(resp.Cleanup)

	go resp.adsReceiveChannel()

	return resp
}

type DeltaAdsTest struct {
	client    DeltaDiscoveryClient
	responses chan *discovery.DeltaDiscoveryResponse
	error     chan error
	t         test.Failer
	conn      *grpc.ClientConn
	metadata  model.NodeMetadata

	ID   string
	Type string

	cancelOnce    sync.Once
	context       context.Context
	cancelContext context.CancelFunc
	timeout       time.Duration
}

func (a *DeltaAdsTest) Cleanup() {
	// Place in once to avoid race when two callers attempt to cleanup
	a.cancelOnce.Do(func() {
		a.cancelContext()
		_ = a.client.CloseSend()
		if a.conn != nil {
			_ = a.conn.Close()
		}
	})
}

func (a *DeltaAdsTest) adsReceiveChannel() {
	go func() {
		<-a.context.Done()
		a.Cleanup()
	}()
	for {
		resp, err := a.client.Recv()
		if err != nil {
			if isUnexpectedError(err) {
				log.Warnf("ads received error: %v", err)
			}
			select {
			case a.error <- err:
			case <-a.context.Done():
			}
			return
		}
		select {
		case a.responses <- resp:
		case <-a.context.Done():
			return
		}
	}
}

// DrainResponses reads all responses, but does nothing to them
func (a *DeltaAdsTest) DrainResponses() {
	a.t.Helper()
	for {
		select {
		case <-a.context.Done():
			return
		case r := <-a.responses:
			log.Infof("drained response %v", r.TypeUrl)
		}
	}
}

// ExpectResponse waits until a response is received and returns it
func (a *DeltaAdsTest) ExpectResponse() *discovery.DeltaDiscoveryResponse {
	a.t.Helper()
	select {
	case <-time.After(a.timeout):
		a.t.Fatalf("did not get response in time")
	case resp := <-a.responses:
		if resp == nil || (len(resp.Resources) == 0 && len(resp.RemovedResources) == 0) {
			a.t.Fatalf("got empty response")
		}
		return resp
	case err := <-a.error:
		a.t.Fatalf("got error: %v", err)
	}
	return nil
}

// ExpectError waits until an error is received and returns it
func (a *DeltaAdsTest) ExpectError() error {
	a.t.Helper()
	select {
	case <-time.After(a.timeout):
		a.t.Fatalf("did not get error in time")
	case err := <-a.error:
		return err
	}
	return nil
}

// ExpectNoResponse waits a short period of time and ensures no response is received
func (a *DeltaAdsTest) ExpectNoResponse() {
	a.t.Helper()
	select {
	case <-time.After(time.Millisecond * 50):
		return
	case resp := <-a.responses:
		a.t.Fatalf("got unexpected response: %v", resp)
	}
}

func (a *DeltaAdsTest) fillInRequestDefaults(req *discovery.DeltaDiscoveryRequest) *discovery.DeltaDiscoveryRequest {
	if req == nil {
		req = &discovery.DeltaDiscoveryRequest{}
	}
	if req.TypeUrl == "" {
		req.TypeUrl = a.Type
	}
	if req.Node == nil {
		req.Node = &core.Node{
			Id:       a.ID,
			Metadata: a.metadata.ToStruct(),
		}
	}
	return req
}

func (a *DeltaAdsTest) Request(req *discovery.DeltaDiscoveryRequest) {
	req = a.fillInRequestDefaults(req)
	if err := a.client.Send(req); err != nil {
		a.t.Fatal(err)
	}
}

// RequestResponseAck does a full XDS exchange: Send a request, get a response, and ACK the response
func (a *DeltaAdsTest) RequestResponseAck(req *discovery.DeltaDiscoveryRequest) *discovery.DeltaDiscoveryResponse {
	a.t.Helper()
	req = a.fillInRequestDefaults(req)
	a.Request(req)
	resp := a.ExpectResponse()
	req.ResponseNonce = resp.Nonce
	a.Request(&discovery.DeltaDiscoveryRequest{
		Node:          req.Node,
		TypeUrl:       req.TypeUrl,
		ResponseNonce: req.ResponseNonce,
	})
	return resp
}

// RequestResponseNack does a full XDS exchange with an error: Send a request, get a response, and NACK the response
func (a *DeltaAdsTest) RequestResponseNack(req *discovery.DeltaDiscoveryRequest) *discovery.DeltaDiscoveryResponse {
	a.t.Helper()
	if req == nil {
		req = &discovery.DeltaDiscoveryRequest{}
	}
	a.Request(req)
	resp := a.ExpectResponse()
	a.Request(&discovery.DeltaDiscoveryRequest{
		Node:          req.Node,
		TypeUrl:       req.TypeUrl,
		ResponseNonce: req.ResponseNonce,
		ErrorDetail:   &status.Status{Message: "Test request NACK"},
	})
	return resp
}

func (a *DeltaAdsTest) WithID(id string) *DeltaAdsTest {
	a.ID = id
	return a
}

func (a *DeltaAdsTest) WithType(typeURL string) *DeltaAdsTest {
	a.Type = typeURL
	return a
}

func (a *DeltaAdsTest) WithMetadata(m model.NodeMetadata) *DeltaAdsTest {
	a.metadata = m
	return a
}

func (a *DeltaAdsTest) WithTimeout(t time.Duration) *DeltaAdsTest {
	a.timeout = t
	return a
}
