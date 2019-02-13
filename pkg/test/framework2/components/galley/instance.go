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

package galley

import (
	"fmt"

	"istio.io/istio/pkg/test/framework2/components/environment"
	"istio.io/istio/pkg/test/framework2/components/environment/native"
	"istio.io/istio/pkg/test/framework2/resource"
	"istio.io/istio/pkg/test/framework2/runtime"
)

// Instance of Galley
type Instance interface {
	// ApplyConfig applies the given config yaml file via Galley.
	ApplyConfig(yamlText string) error

	// ClearConfig clears all applied config so far.
	ClearConfig() error

	// SetMeshConfig applies the given mesh config yaml file via Galley.
	SetMeshConfig(yamlText string) error

	// WaitForSnapshot waits until the given snapshot is observed for the given type URL.
	WaitForSnapshot(collection string, snapshot ...map[string]interface{}) error
}

// New returns a new Galley instance.
func New(s resource.Context) (Instance, error) {
	fmt.Printf("---- New\n")
	switch s.Environment().Name() {
		case native.Name:
			return newNative(s, s.Environment().(*native.Environment))
		default:
			return nil, environment.UnsupportedEnvironment(s.Environment().Name())
	}
}

// NewOrFail returns a new Galley instance, or fails.
func NewOrFail(c *runtime.TestContext) (Instance) {
	i, err := New(c)
	if err != nil {
		c.T().Fatalf("Error creating Galley: %v", err)
	}

	return i
}