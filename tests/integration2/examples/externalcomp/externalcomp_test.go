//  Copyright 2018 Istio Authors
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

// Package basic contains an example test suite for showcase purposes.
package externalcomp

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/environments/local"
)

func TestMain(m *testing.M) {
	framework.Run("externalcomponent_test", m)
}

type MyComponent interface {
	DoStuff()
}

type myLocalComponent struct {
	impl *local.Implementation
}

var _ MyComponent = &myLocalComponent{}

func (m *myLocalComponent) DoStuff() {
	// Do local specific stuff here
}

type myKubernetesComponent struct {
	impl *kubernetes.Implementation
}

var _ MyComponent = &myKubernetesComponent{}

func (m *myKubernetesComponent) DoStuff() {
	// Do kubernetes specific stuff here

}

func NewMyComponent(t testing.TB, ctx environment.ComponentContext) MyComponent {
	t.Helper()

	switch impl := ctx.Environment().(type) {
	case *kubernetes.Implementation:
		return &myKubernetesComponent{
			impl: impl,
		}

	case *local.Implementation:
		return &myLocalComponent{
			impl: impl,
		}

	default:
		t.Fatal("Supported environment not found.")
		return nil
	}
}

func TestBasic(t *testing.T) {
	// Call test.Requires to explicitly initialize dependencies that the test needs.
	framework.Requires(t, dependency.Mixer)

	env := framework.AcquireEnvironment(t)

	m := NewMyComponent(t, env.ComponentContext())

	m.DoStuff()
}
