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

package endpointpicker

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance represents a deployed endpoint picker service (EPP).
// The endpoint picker is an external processor that implements the Envoy ext_proc protocol
// for selecting backend endpoints based on request metadata.
type Instance interface {
	resource.Resource

	// Namespace returns the namespace where the endpoint picker is deployed
	Namespace() namespace.Instance

	// ServiceName returns the Kubernetes service name of the endpoint picker
	ServiceName() string

	// Port returns the gRPC port the endpoint picker listens on
	Port() int
}

// Config defines the configuration for the endpoint picker component.
type Config struct {
	// Namespace to deploy the endpoint picker in. If not set, a new namespace will be created.
	Namespace namespace.Instance

	// ServiceName is the Kubernetes service name. Defaults to "endpoint-picker".
	ServiceName string

	// Port is the gRPC port. Defaults to 9002.
	Port int

	// Image is the container image to use. If not set, uses the test framework's image settings.
	Image string
}

// New returns a new instance of endpoint picker.
func New(ctx resource.Context, c Config) (Instance, error) {
	return newKube(ctx, c)
}

// NewOrFail returns a new endpoint picker instance or fails the test.
func NewOrFail(t test.Failer, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("endpointpicker.NewOrFail: %v", err)
	}
	return i
}
