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

package showcase

import (
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/dependency"
)

var svcCfg = ""

// Reimplement TestSvc2Svc in a_simple-1_test.go
func TestSvcLoading(t *testing.T) {
	// This Requires statement should ensure that all elements are in runnig states
	test.Requires(t, dependency.FortioApps, dependency.Pilot)

	env := test.AcquireEnvironment(t)
	env.Configure(t, svcCfg)

	apps := env.GetFortioApps("app=echosrv", t)

	path := "/echo"
	arg := "load -qps 0 -t 10s "
	// Test Loading
	for _, app := range apps {
		if _, err := app.CallFortio(arg, path); err != nil {
			t.Fatalf("Failed to run fortio %s.", err)
		}
	}
}
