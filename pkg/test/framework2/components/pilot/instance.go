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
	"istio.io/istio/pkg/test/framework2/components/environment"
	"istio.io/istio/pkg/test/framework2/components/environment/native"
	"istio.io/istio/pkg/test/framework2/resource"
	"istio.io/istio/pkg/test/framework2/runtime"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
)

// Instance of Pilot
type Instance interface {
	CallDiscovery(req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error)
	StartDiscovery(req *xdsapi.DiscoveryRequest) error
	WatchDiscovery(duration time.Duration, accept func(*xdsapi.DiscoveryResponse) (bool, error)) error
}

// New returns a new Galley instance.
func New(s resource.Context) (Instance, error) {
	switch s.Environment().Name() {
	case environment.Native:
		return newNative(s, s.Environment().(*native.Environment))
	default:
		return nil, environment.UnsupportedEnvironment(s.Environment().Name())
	}
}

// NewOrFail returns a new Galley instance, or fails.
func NewOrFail(t *testing.T, c *runtime.TestContext) (Instance) {
	t.Helper()
	i, err := New(c)
	if err != nil {
		t.Fatalf("Error creating Galley: %v", err)
	}
	return i
}

