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

// Package gcemetadata provides basic utilities around configuring the fake
// GCE Metadata Server component for integration testing.
package gcemetadata

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance represents a deployed GCE Metadata Server app instance.
type Instance interface {
	// Address is the IP Address of the service provided by the fake GCE
	// Metadata Server.
	Address() string
}

// Config defines the options for creating an fake GCE Metadata Server component.
type Config struct {
	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster
}

// New returns a new instance of stackdriver.
func New(ctx resource.Context, c Config) (i Instance, err error) {
	return newKube(ctx, c)
}

// NewOrFail returns a new GCE Metadata Server instance or fails test.
func NewOrFail(t test.Failer, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("gcemetadata.NewOrFail: %v", err)
	}

	return i
}
