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
	"testing"
	"time"

	"istio.io/istio/pkg/test/util/retry"

	"istio.io/istio/pkg/test/framework/resource"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestAnalysisWritesStatus(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		ns := setupModifiedBookinfo(t, ctx)
		retry.UntilSuccessOrFail(t, func() error { return doTest(t, ctx, ns) },
			retry.Delay(250*time.Millisecond), retry.Timeout(30*time.Second))
	})
}

func setupModifiedBookinfo(t *testing.T, ctx resource.Context) namespace.Instance {
	ns := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix:   "default",
		Inject:   true,
		Revision: "",
		Labels:   nil,
	})
	badVS := `
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: reviews
spec:
  hosts:
  - reviews
  gateways:
  - httpbin-gateway-bogus
  http:
  - route:
    - destination:
        host: reviews
        subset: v1
`
	_, err := deployment.New(ctx, deployment.Config{
		Name:      "bogus-virtualservice",
		Namespace: ns,
		Yaml:      badVS,
	})
	if err != nil {
		t.Fatalf("test setup failure: failed to create bogus virtualservice: %v", err)
	}
	return ns
}

func doTest(t *testing.T, ctx resource.Context, ns namespace.Instance) error {

	x, err := kube.ClusterOrDefault(nil, ctx.Environment()).GetUnstructured(schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "virtualservices",
	}, ns.Name(), "reviews")
	if err != nil {
		t.Fatalf("unexpected test failure: can't get bogus virtualservice: %v", err)
	}

	if x.Object["status"] == nil {
		return fmt.Errorf("object is missing expected status field.  Actual object is: %v", x)
	}
	return nil
}
