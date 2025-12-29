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
	"fmt"
	"strings"
	"testing"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/genproto/googleapis/rpc/status"

	"istio.io/istio/pilot/pkg/model"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestAtMostNJoin(t *testing.T) {
	tests := []struct {
		data  []string
		limit int
		want  string
	}{
		{
			[]string{"a", "b", "c"},
			2,
			"a, and 2 others",
		},
		{
			[]string{"a", "b", "c"},
			4,
			"a, b, c",
		},
		{
			[]string{"a", "b", "c"},
			1,
			"a, b, c",
		},
		{
			[]string{"a", "b", "c"},
			0,
			"a, b, c",
		},
		{
			[]string{},
			3,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s-%d", strings.Join(tt.data, "-"), tt.limit), func(t *testing.T) {
			if got := atMostNJoin(tt.data, tt.limit); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldRespondDelta(t *testing.T) {
	tests := []struct {
		name     string
		wr       map[string]*model.WatchedResource
		request  *discovery.DeltaDiscoveryRequest
		response bool
	}{
		{
			name: "initial request",
			wr:   map[string]*model.WatchedResource{},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl: v3.ClusterType,
			},
			response: true,
		},
		{
			name: "ack",
			wr: map[string]*model.WatchedResource{
				v3.ClusterType: {
					NonceSent: "nonce",
				},
			},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl:       v3.ClusterType,
				ResponseNonce: "nonce",
			},
			response: false,
		},
		{
			name: "ack forced",
			wr: map[string]*model.WatchedResource{
				v3.EndpointType: {
					NonceSent:     "nonce",
					AlwaysRespond: true,
				},
			},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl:       v3.EndpointType,
				ResponseNonce: "nonce",
			},
			response: true,
		},
		{
			name: "stale nonce",
			wr: map[string]*model.WatchedResource{
				v3.ClusterType: {
					NonceSent: "nonce",
				},
			},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl:       v3.ClusterType,
				ResponseNonce: "stale nonce",
			},
			response: false,
		},
		{
			name: "nack",
			wr: map[string]*model.WatchedResource{
				v3.ClusterType: {
					NonceSent: "nonce",
				},
			},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl:     v3.ClusterType,
				ErrorDetail: &status.Status{Message: "Test request NACK"},
			},
			response: false,
		},
		{
			name: "reconnect",
			wr:   map[string]*model.WatchedResource{},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl:                 v3.ClusterType,
				InitialResourceVersions: map[string]string{},
				ResponseNonce:           "reconnect nonce",
			},
			response: true,
		},
		{
			name: "resources subscribe",
			wr: map[string]*model.WatchedResource{
				v3.EndpointType: {
					NonceSent:     "nonce",
					ResourceNames: sets.New("cluster1"),
				},
			},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl:                v3.EndpointType,
				ResponseNonce:          "nonce",
				ResourceNamesSubscribe: []string{"cluster2"},
			},
			response: true,
		},
		{
			name: "resources unsubscribe",
			wr: map[string]*model.WatchedResource{
				v3.EndpointType: {
					NonceSent:     "nonce",
					ResourceNames: sets.New("cluster1"),
				},
			},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl:                  v3.EndpointType,
				ResponseNonce:            "nonce",
				ResourceNamesUnsubscribe: []string{"cluster1"},
			},
			response: true,
		},
		{
			name: "ack with same resources",
			wr: map[string]*model.WatchedResource{
				v3.EndpointType: {
					NonceSent:     "nonce",
					ResourceNames: sets.New("cluster2", "cluster1"),
				},
			},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl:                v3.EndpointType,
				ResponseNonce:          "nonce",
				ResourceNamesSubscribe: []string{"cluster1", "cluster2"},
			},
			response: false,
		},
		{
			name: "initial resources",
			wr:   map[string]*model.WatchedResource{},
			request: &discovery.DeltaDiscoveryRequest{
				TypeUrl:                 v3.EndpointType,
				InitialResourceVersions: map[string]string{"clsuter1": "version1"},
			},
			response: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newConnection("", &fakeStream{})
			conn.SetID("proxy")
			conn.proxy = &model.Proxy{
				WatchedResources: tt.wr,
			}
			assert.Equal(t, shouldRespondDelta(conn, tt.request), tt.response)
		})
	}
}
