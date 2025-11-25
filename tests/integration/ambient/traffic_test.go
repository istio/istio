//go:build integ
// +build integ

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

package ambient

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo/common/deployment"
	"istio.io/istio/tests/integration/pilot/common"
)

func TestTraffic(t *testing.T) {
	framework.NewTest(t).
		TopLevel().
		Run(func(t framework.TestContext) {
			deployments := deployment.NewOrFail(t, deployment.Config{
				IncludeExtAuthz: false,
			})
			SetWaypointServiceEntry(t, "external-service", deployments.NS[0].Namespace.Name(), "waypoint")
			common.RunAllTrafficTests(t, i, deployments.SingleNamespaceView())
		})
}
