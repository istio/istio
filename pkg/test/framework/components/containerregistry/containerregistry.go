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

// Package containerregistry provides basic utilities around configuring the fake
// container registry server component for integration testing.
package containerregistry

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance represents a deployed fake container registry app instance.
type Instance interface {
	// Address is the address of the service provided by the
	// fake container registry server.
	Address() string
}

// Config defines the options for creating an fake container registry component.
type Config struct {
	// Cluster to be used in a multicluster environment
	Cluster cluster.Cluster
}

// New returns a new instance of container registry.
func New(ctx resource.Context, c Config) (i Instance, err error) {
	return newKube(ctx, c)
}

// NewOrFail returns a new container registry instance or fails test.
func NewOrFail(t test.Failer, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("containerregistry.NewOrFail: %v", err)
	}

	return i
}
