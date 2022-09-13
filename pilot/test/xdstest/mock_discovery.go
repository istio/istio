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

package xdstest

import (
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"istio.io/istio/pkg/test"
	"istio.io/pkg/log"
)

// MockDiscovery is a DiscoveryServer that allows users full control over responses.
type MockDiscovery struct {
	Listener       *bufconn.Listener
	responses      chan *discovery.DiscoveryResponse
	deltaResponses chan *discovery.DeltaDiscoveryResponse
	close          chan struct{}
}

func NewMockServer(t test.Failer) *MockDiscovery {
	s := &MockDiscovery{
		close:          make(chan struct{}),
		responses:      make(chan *discovery.DiscoveryResponse),
		deltaResponses: make(chan *discovery.DeltaDiscoveryResponse),
	}

	buffer := 1024 * 1024
	listener := bufconn.Listen(buffer)
	grpcServer := grpc.NewServer()
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, s)
	go func() {
		if err := grpcServer.Serve(listener); err != nil && !(err == grpc.ErrServerStopped || err.Error() == "closed") {
			t.Fatal(err)
		}
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		close(s.close)
	})
	s.Listener = listener
	return s
}

func (f *MockDiscovery) StreamAggregatedResources(server discovery.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	numberOfSends := 0
	for {
		select {
		case <-f.close:
			return nil
		case resp := <-f.responses:
			numberOfSends++
			log.Infof("sending response from mock: %v", numberOfSends)
			if err := server.Send(resp); err != nil {
				return err
			}
		}
	}
}

func (f *MockDiscovery) DeltaAggregatedResources(server discovery.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	numberOfSends := 0
	for {
		select {
		case <-f.close:
			return nil
		case resp := <-f.deltaResponses:
			numberOfSends++
			log.Infof("sending delta response from mock: %v", numberOfSends)
			if err := server.Send(resp); err != nil {
				return err
			}
		}
	}
}

// SendResponse sends a response to a (random) client. This can block if sends are blocked.
func (f *MockDiscovery) SendResponse(dr *discovery.DiscoveryResponse) {
	f.responses <- dr
}

// SendDeltaResponse sends a response to a (random) client. This can block if sends are blocked.
func (f *MockDiscovery) SendDeltaResponse(dr *discovery.DeltaDiscoveryResponse) {
	f.deltaResponses <- dr
}

var _ discovery.AggregatedDiscoveryServiceServer = &MockDiscovery{}
