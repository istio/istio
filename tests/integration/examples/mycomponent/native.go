// Copyright 2019 Istio Authors
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

package mycomponent

import (
	"io"

	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/resource"
)

type nativeComponent struct {
	id resource.ID
}

var _ Instance = &nativeComponent{}

// Implement the resource.Resource interface to allow tracking of your resources.
var _ resource.Resource = &nativeComponent{}

// If the component implements io.Closer, then the framework will call Close method to cleanup resources.
var _ io.Closer = &nativeComponent{}

// If the component implements resource.Dumper, then the framework will call this method to cause the component
// dump detailed diagnostic information to log/filesystem for further debugging by the user. This is very useful
// when debugging tests that fail in CI.
var _ resource.Dumper = &nativeComponent{}

// If the component implements resource.Resetter, then the framework will call Reset whenever a *sub* context
// gets closed. This is useful for creating suite-level resources (or top-level test resources) that needs to
// reset their state after the execution of each test.
var _ resource.Resetter = &nativeComponent{}

// ID implements resource.Resource.
func (n *nativeComponent) ID() resource.ID {
	return n.id
}

// DoStuff implements Instance
func (n *nativeComponent) DoStuff() error {
	return nil
}

// Close implements io.Closer
func (n *nativeComponent) Close() error {
	return nil
}

// Dump implements resource.Dumper
func (n *nativeComponent) Dump() {
	// Dump diagnostic information to the file system. Allocate directories via resource.Context, so that they will
	// get captured by the CI system as artifacts.
}

// Reset implements resource.Resetter
func (n *nativeComponent) Reset() error {
	return nil
}

func newNative(ctx resource.Context, _ Config) (Instance, error) {
	i := &nativeComponent{}
	// After creating your resource, immediately register it with the context for tracking
	ctx.TrackResource(i)

	// You can also side-cast to reach the environment and perform environment-specific operations
	env := ctx.Environment().(*native.Environment)
	_ = env

	return i, nil
}
