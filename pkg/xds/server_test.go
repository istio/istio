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
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/util/sets"
)

type TestProxy struct {
	WatchedResources map[string]*WatchedResource
}

func (p *TestProxy) DeleteWatchedResource(url string) {
	delete(p.WatchedResources, url)
}

func (p *TestProxy) GetWatchedResource(url string) *WatchedResource {
	return p.WatchedResources[url]
}

func (p *TestProxy) NewWatchedResource(url string, names []string) {
	p.WatchedResources[url] = &WatchedResource{ResourceNames: sets.New(names...)}
}

func (p *TestProxy) UpdateWatchedResource(url string, f func(*WatchedResource) *WatchedResource) {
	p.WatchedResources[url] = f(p.WatchedResources[url])
}

func (p *TestProxy) GetID() string {
	return ""
}

func TestShouldRespond(t *testing.T) {
	tests := []struct {
		name     string
		proxy    *TestProxy
		request  *discovery.DiscoveryRequest
		response bool
	}{
		{
			name: "initial request",
			proxy: &TestProxy{
				WatchedResources: map[string]*WatchedResource{},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl: model.ClusterType,
			},
			response: true,
		},
		{
			name: "ack",
			proxy: &TestProxy{
				WatchedResources: map[string]*WatchedResource{
					model.ClusterType: {
						NonceSent: "nonce",
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       model.ClusterType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
			},
			response: false,
		},
		{
			name: "ack forced",
			proxy: &TestProxy{
				WatchedResources: map[string]*WatchedResource{
					model.EndpointType: {
						NonceSent:     "nonce",
						AlwaysRespond: true,
						ResourceNames: sets.New("my-resource"),
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       model.EndpointType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
				ResourceNames: []string{"my-resource"},
			},
			response: true,
		},
		{
			name: "nack",
			proxy: &TestProxy{
				WatchedResources: map[string]*WatchedResource{
					model.ClusterType: {
						NonceSent: "nonce",
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       model.ClusterType,
				VersionInfo:   "v1",
				ResponseNonce: "stale nonce",
			},
			response: false,
		},
		{
			name: "reconnect",
			proxy: &TestProxy{
				WatchedResources: map[string]*WatchedResource{},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       model.ClusterType,
				VersionInfo:   "v1",
				ResponseNonce: "reconnect nonce",
			},
			response: true,
		},
		{
			name: "resources change",
			proxy: &TestProxy{
				WatchedResources: map[string]*WatchedResource{
					model.EndpointType: {
						NonceSent:     "nonce",
						ResourceNames: sets.New("cluster1"),
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       model.EndpointType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
				ResourceNames: []string{"cluster1", "cluster2"},
			},
			response: true,
		},
		{
			name: "ack with same resources",
			proxy: &TestProxy{
				WatchedResources: map[string]*WatchedResource{
					model.EndpointType: {
						NonceSent:     "nonce",
						ResourceNames: sets.New("cluster2", "cluster1"),
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       model.EndpointType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
				ResourceNames: []string{"cluster1", "cluster2"},
			},
			response: false,
		},
		{
			name: "unsubscribe EDS",
			proxy: &TestProxy{
				WatchedResources: map[string]*WatchedResource{
					model.EndpointType: {
						NonceSent:     "nonce",
						ResourceNames: sets.New("cluster2", "cluster1"),
					},
				},
			},
			request: &discovery.DiscoveryRequest{
				TypeUrl:       model.EndpointType,
				VersionInfo:   "v1",
				ResponseNonce: "nonce",
				ResourceNames: []string{},
			},
			response: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if response, _ := ShouldRespond(tt.proxy, "test", tt.request); response != tt.response {
				t.Fatalf("Unexpected value for response, expected %v, got %v", tt.response, response)
			}
			if tt.name != "reconnect" && tt.response {
				if tt.proxy.WatchedResources[tt.request.TypeUrl].NonceAcked != tt.request.ResponseNonce {
					t.Fatal("Version & Nonce not updated properly")
				}
			}
		})
	}
}
