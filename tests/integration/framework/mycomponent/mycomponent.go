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

package mycomponent

import (
	"testing"

	"istio.io/istio/pkg/test/framework/resource"
)

// Instance is an example component. Consider implementing your component as an interface, so that others can add
// environment specific implementations.
type Instance interface {
	// DoStuff that matters, or returns an error if it fails.
	DoStuff() error
}

// Config allows you to pass test/suite specific configuration during initialization.
type Config struct {
	DoStuffElegantly bool
}

// New is the canonical method the users will look for creating a new instance of component. It should be possible
// (and easy) to create multiple instance of your component.
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	return newKube(ctx, cfg), nil
}

// NewOrFail is a very useful convenience for allocating components within tests.
func NewOrFail(t *testing.T, ctx resource.Context, cfg Config) Instance {
	t.Helper()

	i, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("mycomponent.NewOrFail: %v", err)
	}

	return i
}
