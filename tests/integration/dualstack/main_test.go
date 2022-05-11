//go:build integ
// +build integ

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

package dualstack

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/tests/integration/dualstack/common"
)

var (
	istioInst istio.Instance
	apps      map[string]*common.EchoDeployments
)

// TestMain defines the entrypoint for dualstack tests using a dualstack Istio installation.
// If a test requires a custom install it should go into its own package, otherwise it should go
// here to reuse a single install across tests.
func TestMain(m *testing.M) {
	apps = make(map[string]*common.EchoDeployments)
	framework.
		NewSuite(m).
		RequireDualStackCluster().
		Setup(istio.Setup(&istioInst, nil)).
		Setup(func(ctx resource.Context) error {
			return common.SetupApps(ctx, apps)
		}).
		Setup(common.SetupPrometheus).
		Run()
}
