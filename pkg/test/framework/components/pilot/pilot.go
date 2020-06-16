//  Copyright Istio Authors
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
	"fmt"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	meshConfig "istio.io/api/mesh/v1alpha1"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

// TypeURL for making discovery requests.
type TypeURL string

// Instance of Pilot
type Instance interface {
	resource.Resource

	CallDiscovery(req *discovery.DiscoveryRequest) (*discovery.DiscoveryResponse, error)
	CallDiscoveryOrFail(t test.Failer, req *discovery.DiscoveryRequest) *discovery.DiscoveryResponse

	StartDiscovery(req *discovery.DiscoveryRequest) error
	StartDiscoveryOrFail(t test.Failer, req *discovery.DiscoveryRequest)
	WatchDiscovery(duration time.Duration, accept func(*discovery.DiscoveryResponse) (bool, error)) error
	WatchDiscoveryOrFail(t test.Failer, duration time.Duration, accept func(*discovery.DiscoveryResponse) (bool, error))
}

// Structured config for the Pilot component
type Config struct {
	fmt.Stringer

	// The MeshConfig to be used for Pilot in native environment. In Kube environment this can be
	// configured with Helm.
	MeshConfig *meshConfig.MeshConfig

	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster
}

// New returns a new instance of echo.
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Kube, func() {
		i, err = newKube(ctx, cfg)
	})
	return
}

// NewOrFail returns a new Pilot instance, or fails test.
func NewOrFail(t test.Failer, c resource.Context, config Config) Instance {
	i, err := New(c, config)
	if err != nil {
		t.Fatalf("pilot.NewOrFail: %v", err)
	}
	return i
}

// NewDiscoveryRequest is a utility method for creating a new request for the given node and type.
func NewDiscoveryRequest(nodeID string, typeURL TypeURL) *discovery.DiscoveryRequest {
	return &discovery.DiscoveryRequest{
		Node: &core.Node{
			Id: nodeID,
		},
		TypeUrl: string(typeURL),
	}
}
