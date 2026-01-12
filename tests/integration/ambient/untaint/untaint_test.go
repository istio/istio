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

package untaint

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/util/retry"
)

func TestTaintsRemoved(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			// make cni not deploy to one of the nodes
			taintNodes(ctx)

			// make sure all nodes were untainted
			retry.UntilSuccessOrFail(t, func() error {
				nodeC := ctx.Clusters().Default().Kube().CoreV1().Nodes()
				nodes, err := nodeC.List(ctx.Context(), metav1.ListOptions{})
				if err != nil {
					return err
				}

				for _, node := range nodes.Items {
					for _, taint := range node.Spec.Taints {
						if taint.Key == "cni.istio.io/not-ready" {
							return fmt.Errorf("node %v still has taint %v", node.Name, taint)
						}
					}
				}
				return nil
			}, retry.Delay(1*time.Second), retry.Timeout(80*time.Second))
		})
}
