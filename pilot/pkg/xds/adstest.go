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
	sds "github.com/envoyproxy/go-control-plane/envoy/service/secret/v3"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test"
)

func NewAdsTest(t test.Failer, conn *grpc.ClientConn) *AdsTest {
	return NewXdsTest(t, conn, func(conn *grpc.ClientConn) (DiscoveryClient, error) {
		xds := discovery.NewAggregatedDiscoveryServiceClient(conn)
		return xds.StreamAggregatedResources(context.Background())
	})
}

func NewSdsTest(t test.Failer, conn *grpc.ClientConn) *AdsTest {
	return NewXdsTest(t, conn, func(conn *grpc.ClientConn) (DiscoveryClient, error) {
		xds := sds.NewSecretDiscoveryServiceClient(conn)
		return xds.StreamSecrets(context.Background())
	}).WithType(v3.SecretType)
}

func NewXdsTest(t test.Failer, conn *grpc.ClientConn, getClient func(conn *grpc.ClientConn) (DiscoveryClient, error)) *AdsTest {
	ctx, cancel := context.WithCancel(context.Background())

	cl, err := getClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	resp := &AdsTest{
		client:        cl,
		conn:          conn,
		context:       ctx,
		cancelContext: cancel,
		ID:            "sidecar~1.1.1.1~test.default~default.svc.cluster.local",
		timeout:       time.Second,
		Type:          v3.ClusterType,
		responses:     make(chan *discovery.DiscoveryResponse),
		error:         make(chan error),
	}
	t.Cleanup(resp.Cleanup)

	go resp.adsReceiveChannel()

	return resp
}

type AdsTest struct {
	client    DiscoveryClient
	responses chan *discovery.DiscoveryResponse
	error     chan error
	conn      *grpc.ClientConn
	metadata  model.NodeMetadata

	ID   string
	Type string

	cancelOnce    sync.Once
	context       context.Context
	cancelContext context.CancelFunc
	timeout       time.Duration
}

func (a *AdsTest) Cleanup() {
	// Place in once to avoid race when two callers attempt to cleanup
	a.cancelOnce.Do(func() {
		a.cancelContext()
		_ = a.client.CloseSend()
		if a.conn != nil {
			_ = a.conn.Close()
		}
	})
}

func (a *AdsTest) adsReceiveChannel() {
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
func (a *AdsTest) DrainResponses() {
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
func (a *AdsTest) ExpectResponse(t test.Failer) *discovery.DiscoveryResponse {
	t.Helper()
	select {
	case <-time.After(a.timeout):
		t.Fatalf("did not get response in time")
	case resp := <-a.responses:
		if resp == nil || len(resp.Resources) == 0 {
			t.Fatalf("got empty response")
		}
		return resp
	case err := <-a.error:
		t.Fatalf("got error: %v", err)
	}
	return nil
}

// ExpectError waits until an error is received and returns it
func (a *AdsTest) ExpectError(t test.Failer) error {
	t.Helper()
	select {
	case <-time.After(a.timeout):
		t.Fatalf("did not get error in time")
	case err := <-a.error:
		return err
	}
	return nil
}

// ExpectNoResponse waits a short period of time and ensures no response is received
func (a *AdsTest) ExpectNoResponse(t test.Failer) {
	t.Helper()
	select {
	case <-time.After(time.Millisecond * 50):
		return
	case resp := <-a.responses:
		t.Fatalf("got unexpected response: %v", resp)
	}
}

func (a *AdsTest) fillInRequestDefaults(req *discovery.DiscoveryRequest) *discovery.DiscoveryRequest {
	if req == nil {
		req = &discovery.DiscoveryRequest{}
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

func (a *AdsTest) Request(t test.Failer, req *discovery.DiscoveryRequest) {
	t.Helper()
	req = a.fillInRequestDefaults(req)
	if err := a.client.Send(req); err != nil {
		t.Fatal(err)
	}
}

// RequestResponseAck does a full XDS exchange: Send a request, get a response, and ACK the response
func (a *AdsTest) RequestResponseAck(t test.Failer, req *discovery.DiscoveryRequest) *discovery.DiscoveryResponse {
	t.Helper()
	req = a.fillInRequestDefaults(req)
	a.Request(t, req)
	resp := a.ExpectResponse(t)
	req.ResponseNonce = resp.Nonce
	req.VersionInfo = resp.VersionInfo
	a.Request(t, req)
	return resp
}

// RequestResponseAck does a full XDS exchange with an error: Send a request, get a response, and NACK the response
func (a *AdsTest) RequestResponseNack(t test.Failer, req *discovery.DiscoveryRequest) *discovery.DiscoveryResponse {
	t.Helper()
	if req == nil {
		req = &discovery.DiscoveryRequest{}
	}
	a.Request(t, req)
	resp := a.ExpectResponse(t)
	req.ResponseNonce = resp.Nonce
	req.ErrorDetail = &status.Status{Message: "Test request NACK"}
	a.Request(t, req)
	return resp
}

func (a *AdsTest) WithID(id string) *AdsTest {
	a.ID = id
	return a
}

func (a *AdsTest) WithType(typeURL string) *AdsTest {
	a.Type = typeURL
	return a
}

func (a *AdsTest) WithMetadata(m model.NodeMetadata) *AdsTest {
	a.metadata = m
	return a
}

func (a *AdsTest) WithTimeout(t time.Duration) *AdsTest {
	a.timeout = t
	return a
}
