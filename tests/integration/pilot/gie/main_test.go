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

package gie

import (
	"context"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

var i istio.Instance

func TestMain(m *testing.M) {
	framework.
		NewSuite(m).
		Label(label.Full).
		Setup(istio.Setup(&i, func(ctx resource.Context, cfg *istio.Config) {
			cfg.ControlPlaneValues = `
values:
  pilot:
    env:
      ENABLE_GATEWAY_API_INFERENCE_EXTENSION: "true"
`
		})).
		Run()
}

// waitForGatewayProgrammed blocks until the Gateway reports both Accepted and Programmed.
func waitForGatewayProgrammed(ctx framework.TestContext, ns, name string) {
	ctx.Helper()
	retry.UntilSuccessOrFail(ctx, func() error {
		gw, err := ctx.Clusters().Default().Dynamic().Resource(schema.GroupVersionResource{
			Group:    "gateway.networking.k8s.io",
			Version:  "v1",
			Resource: "gateways",
		}).Namespace(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("gateway resource not found: %v", err)
		}

		conditions, found, err := unstructured.NestedSlice(gw.Object, "status", "conditions")
		if err != nil || !found {
			return fmt.Errorf("gateway status conditions not found")
		}

		accepted, programmed := false, false
		for _, cond := range conditions {
			condition, ok := cond.(map[string]any)
			if !ok {
				continue
			}
			switch condition["type"] {
			case "Accepted":
				accepted = condition["status"] == "True"
			case "Programmed":
				programmed = condition["status"] == "True"
			}
		}
		if !accepted {
			return fmt.Errorf("gateway not accepted yet")
		}
		if !programmed {
			return fmt.Errorf("gateway not programmed yet")
		}
		return nil
	}, retry.Timeout(60*time.Second))
}
