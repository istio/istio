// Copyright 2019 Istio Authors
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
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/test/framework/resource"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestStatusExistsByDefault(t *testing.T) {
	// This test is not yet implemented
	framework.NewTest(t).
		NotImplementedYet("Usability.Observability.Status.DefaultExists")
}

func TestAnalysisWritesStatus(t *testing.T) {
	framework.NewTest(t).
	LabelFeatures("Usability.Observability.Status").
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
		g.ApplyConfigOrFail(t, ns, `
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
		})
		// Apply config to make this not invalid
		g.ApplyConfigOrFail(t, ns, `
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
	x, err := kube.ClusterOrDefault(nil, ctx.Environment()).GetUnstructured(schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "virtualservices",
	}, ns.Name(), "reviews")
	if err != nil {
		t.Fatalf("unexpected test failure: can't get bogus virtualservice: %v", err)
	}

	if hasError && x.Object["status"] == nil {
		return fmt.Errorf("object is missing expected status field.  Actual object is: %v", x)
	}
	status := fmt.Sprintf("%v", x.Object["status"])
	if strings.Contains(status, msg.ReferencedResourceNotFound.Code()) != hasError {
		return fmt.Errorf("expected error=%v, but got %v", hasError, status)
	}
	return nil
}
