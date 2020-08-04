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

package analysis

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	status2 "istio.io/istio/pilot/pkg/status"

	"istio.io/istio/pkg/test/framework/features"

	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/test/framework/resource"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestStatusExistsByDefault(t *testing.T) {
	// This test is not yet implemented
	framework.NewTest(t).
		NotImplementedYet(features.Usability_Observability_Status_DefaultExists)
}

func TestAnalysisWritesStatus(t *testing.T) {
	framework.NewTest(t).
		Features(features.Usability_Observability_Status).
		// TODO: make feature labels heirarchical constants like:
		// Label(features.Usability.Observability.Status).
		Run(func(ctx framework.TestContext) {
			ns := namespace.NewOrFail(t, ctx, namespace.Config{
				Prefix:   "default",
				Inject:   true,
				Revision: "",
				Labels:   nil,
			})
			// Apply bad config (referencing invalid host)
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: reviews
spec:
  gateways: [missing-gw]
  hosts:
  - reviews
  http:
  - route:
    - destination: 
        host: reviews
`)
			// Status should report error
			retry.UntilSuccessOrFail(t, func() error {
				return expectStatus(t, ctx, ns, true)
			}, retry.Timeout(time.Minute*5))
			// Apply config to make this not invalid
			ctx.Config().ApplyYAMLOrFail(t, ns.Name(), `
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: missing-gw
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 80
      name: http
      protocol: HTTP
    hosts:
    - "*"
`)
			// Status should no longer report error
			retry.UntilSuccessOrFail(t, func() error {
				return expectStatus(t, ctx, ns, false)
			})
		})
}

func expectStatus(t *testing.T, ctx resource.Context, ns namespace.Instance, hasError bool) error {
	c := ctx.Clusters().Default()
	gvr := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "virtualservices",
	}
	x, err := c.Dynamic().Resource(gvr).Namespace(ns.Name()).Get(context.TODO(), "reviews", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected test failure: can't get bogus virtualservice: %v", err)
	}

	if hasError && x.Object["status"] == nil {
		return fmt.Errorf("object is missing expected status field.  Actual object is: %v", x)
	}
	statusString := fmt.Sprintf("%v", x.Object["status"])
	if strings.Contains(statusString, msg.ReferencedResourceNotFound.Code()) != hasError {
		return fmt.Errorf("expected error=%v, but got %v", hasError, statusString)
	}

	status, err := status2.GetTypedStatus(x.Object["status"])
	if err != nil {
		return fmt.Errorf("unable to cast status field '%s'to istiostatus:, %v", statusString, err)
	}
	if len(status.Conditions) < 1 {
		return fmt.Errorf("expected conditions to exist, but got %v", status)
	}
	found := false
	for _, condition := range status.Conditions {
		if condition.Type == status2.Reconciled {
			found = true
			if condition.Status != metav1.ConditionTrue {
				return fmt.Errorf("expected Reconciled to be true but was %v", condition.Status)
			}
		}
	}
	if !found {
		return fmt.Errorf("expected Reconciled condition to exist, but got %v", status.Conditions)
	}
	return nil
}
