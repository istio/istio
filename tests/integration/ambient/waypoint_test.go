//go:build integ

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
	"context"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8s "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/model/kstatus"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
)

func TestWaypointStatus(t *testing.T) {
	framework.
		NewTest(t).
		Features("traffic.ambient").
		Run(func(t framework.TestContext) {
			client := t.Clusters().Kube().Default().GatewayAPI().GatewayV1beta1().GatewayClasses()

			check := func() error {
				gwc, _ := client.Get(context.Background(), constants.WaypointGatewayClassName, metav1.GetOptions{})
				if gwc == nil {
					return fmt.Errorf("failed to find GatewayClass %v", constants.WaypointGatewayClassName)
				}
				cond := kstatus.GetCondition(gwc.Status.Conditions, string(k8s.GatewayClassConditionStatusAccepted))
				if cond.Status != metav1.ConditionTrue {
					return fmt.Errorf("failed to find accepted condition: %+v", cond)
				}
				if cond.ObservedGeneration != gwc.Generation {
					return fmt.Errorf("stale GWC generation: %+v", cond)
				}
				return nil
			}
			retry.UntilSuccessOrFail(t, check)

			// Wipe out the status
			gwc, _ := client.Get(context.Background(), constants.WaypointGatewayClassName, metav1.GetOptions{})
			gwc.Status.Conditions = nil
			client.Update(context.Background(), gwc, metav1.UpdateOptions{})
			// It should be added back
			retry.UntilSuccessOrFail(t, check)
		})
}
