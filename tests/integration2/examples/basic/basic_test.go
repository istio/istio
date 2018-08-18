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
package basic

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/dependency"
)

// To opt-in to the test framework, implement a TestMain, and call test.Run.
func TestMain(m *testing.M) {
	framework.Run("basic_test", m)
}

func TestBasic(t *testing.T) {
	// Call test.Requires to explicitly initialize dependencies that the test needs.
	framework.Requires(t, dependency.Mixer)

	// To access dependencies, and environment specific-functionality, call test.AcquireEnvironment.
	// This cleans up any environment specific settings and reinitializes the dependencies internally.
	env := framework.AcquireEnvironment(t)

	// Call environment.Configure to set Istio-wide configuration for the test.
	env.Configure(t, `
apiVersion: "config.istio.io/v1alpha2"
kind: metric
metadata:
 name: metric1
spec:
 value: "2"
 dimensions:
   source: source.name | "mysrc"
   target_ip: destination.name | "mytarget"
`)

	// Call environment.GetXXXOrFail methods to get handles to Istio components. From here, you can perform
	// high level operations against these components.
	m := env.GetMixerOrFail(t)

	// As an example, the following method calls the Report operation against Mixer's own API directly.
	m.Report(t, map[string]interface{}{})
}
