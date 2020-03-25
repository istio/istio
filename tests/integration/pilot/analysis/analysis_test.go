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
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/bookinfo"
	"istio.io/istio/pkg/test/framework/components/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

func TestAnalysisWritesStatus(t *testing.T) {
	framework.NewTest(t).Run(func(ctx framework.TestContext) {
		ns := setupModifiedBookinfo(t, ctx)
		doTest(t, ctx, ns)
	})
}

func setupModifiedBookinfo(t *testing.T, ctx framework.TestContext) namespace.Instance {
	ns := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix:   "default",
		Inject:   true,
		Revision: "",
		Labels:   nil,
	})
	bookinfo.DeployOrFail(t, ctx, bookinfo.Config{
		Namespace: ns,
		Cfg:       bookinfo.BookinfoRatingsv2,
	})
	gatewayYaml := bookinfo.NetworkingBookinfoGateway.LoadGatewayFileWithNamespaceOrFail(t, ns.Name())
	_, err := deployment.New(ctx, deployment.Config{
		Name:      "bookinfo-gateway",
		Namespace: ns,
		Yaml:      gatewayYaml,
	})
	if err != nil {
		t.Fatalf("TODO: make a better failure message: %v", err)
	}
	vsYaml := bookinfo.NetworkingVirtualServiceAllV1.LoadWithNamespaceOrFail(t, ns.Name())
	// mutate the virtual service to trigger an analyzer message
	vsYaml = strings.Replace(vsYaml, "old", "new", -1)
	_, err = deployment.New(ctx, deployment.Config{
		Name:      "bookinfo-virtualservice",
		Namespace: ns,
		Yaml:      vsYaml,
	})
	return ns
}

func doTest(t *testing.T, ctx framework.TestContext, ns namespace.Instance) {
	x, err := kube.ClusterOrDefault(nil, ctx.Environment()).GetUnstructured(schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1alpha3",
		Resource: "virtualservices",
	}, ns.Name(), "reviews")
	if err != nil {
		t.Fatalf("TODO: another error message here: %v", err)
	}

	if x.Object["status"] == nil {
		t.Fatalf("Object is missing expected status field.  Actual object is: %v", x)
	}
}
