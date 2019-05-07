//  Copyright 2019 Istio Authors
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
	"testing"
	"time"

	"istio.io/istio/pkg/test/framework/components/environment"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	xdscore "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"

	meshConfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/test/framework/components/galley"
	"istio.io/istio/pkg/test/framework/resource"
)

// TypeURL for making discovery requests.
type TypeURL string

const (
	typeURLPrefix                 = "type.googleapis.com/envoy.api.v2."
	Listener              TypeURL = typeURLPrefix + "Listener"
	Cluster               TypeURL = typeURLPrefix + "Cluster"
	ClusterLoadAssignment TypeURL = typeURLPrefix + "ClusterLoadAssignment"
	Route                 TypeURL = typeURLPrefix + "RouteConfiguration"
)

// Instance of Pilot
type Instance interface {
	resource.Resource

	CallDiscovery(req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error)
	CallDiscoveryOrFail(t testing.TB, req *xdsapi.DiscoveryRequest) *xdsapi.DiscoveryResponse

	StartDiscovery(req *xdsapi.DiscoveryRequest) error
	StartDiscoveryOrFail(t testing.TB, req *xdsapi.DiscoveryRequest)
	WatchDiscovery(duration time.Duration, accept func(*xdsapi.DiscoveryResponse) (bool, error)) error
	WatchDiscoveryOrFail(t testing.TB, duration time.Duration, accept func(*xdsapi.DiscoveryResponse) (bool, error))
}

// Structured config for the Pilot component
type Config struct {
	fmt.Stringer
	// If set then pilot takes a dependency on the referenced Galley instance
	Galley galley.Instance

	// The MeshConfig to be used for Pilot in native environment. In Kube environment this can be
	// configured with Helm.
	MeshConfig *meshConfig.MeshConfig
}

// New returns a new instance of echo.
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Native, func() {
		i, err = newNative(ctx, cfg)
	})
	ctx.Environment().Case(environment.Kube, func() {
		i, err = newKube(ctx, cfg)
	})
	return
}

// NewOrFail returns a new Pilot instance, or fails test.
func NewOrFail(t *testing.T, c resource.Context, config Config) Instance {
	i, err := New(c, config)
	if err != nil {
		t.Fatalf("pilot.NewOrFail: %v", err)
	}
	return i
}

// NewDiscoveryRequest is a utility method for creating a new request for the given node and type.
func NewDiscoveryRequest(nodeID string, typeURL TypeURL) *xdsapi.DiscoveryRequest {
	return &xdsapi.DiscoveryRequest{
		Node: &xdscore.Node{
			Id: nodeID,
		},
		TypeUrl: string(typeURL),
	}
}
