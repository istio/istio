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

package upgrade

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/pkg/log"
	"strings"
	"testing"
)

func TestRevisionedUpgrade(t *testing.T) {
	framework.
		NewTest(t).
		Run(func(ctx framework.TestContext) {
			t.Run(fmt.Sprintf("%s->master", NMinusOne.Settings().Version), func(t *testing.T) {
				testUpgradeFromVersion(ctx, t, NMinusOne.Settings().Version, NMinusOne.Settings().Revision)
			})
			t.Run(fmt.Sprintf("%s->master", NMinusTwo.Settings().Version), func(t *testing.T) {
				testUpgradeFromVersion(ctx, t, NMinusTwo.Settings().Version, NMinusTwo.Settings().Revision)
			})
	})
}

// testUpgradeFromVersion tests an upgrade from the target revision to the namespace running master revision
func testUpgradeFromVersion(ctx framework.TestContext, t *testing.T, version, revision string) {
	revisionedNamespace := namespace.NewOrFail(t, ctx, namespace.Config{
		Prefix:   revision,
		Inject:   true,
		Revision: revision,
	})

	var revisionedInstance echo.Instance
	builder := echoboot.NewBuilder(ctx)
	builder.With(&revisionedInstance, echo.Config{
		Service:   fmt.Sprintf("svc-%s", revision),
		Namespace: revisionedNamespace,
		Ports: []echo.Port{
			{
				Name:         "http",
				Protocol:     protocol.HTTP,
				InstancePort: 8080,
			},
		},
	})
	builder.BuildOrFail(t)
	sendSimpleTrafficOrFail(t, revisionedInstance)

	if err := enableDefaultInjection(revisionedNamespace); err != nil {
		ctx.Fatalf("could not relabel namespace to enable default injection: %v", err)
	}

	log.Infof("rolling out echo workloads behind service %q", revisionedInstance.Config().Service)
	if err := revisionedInstance.Restart(); err != nil {
		ctx.Fatalf("revisioned instance rollout failed with: %v", err)
	}
	fetch := kubetest.NewPodMustFetch(ctx.Clusters().Default(), revisionedInstance.Config().Namespace.Name(), fmt.Sprintf("app=%s", revisionedInstance.Config().Service)) // nolint: lll
	pods, err := kubetest.CheckPodsAreReady(fetch)
	if err != nil {
		ctx.Fatalf("failed to retrieve upgraded pods: %v", err)
	}
	for _, p := range pods {
		for _, c := range p.Spec.Containers {
			if strings.Contains(c.Image, version) {
				ctx.Fatalf("expected post-upgrade container image not to include %q, got %s", version, c.Image)
			}
		}
	}

	sendSimpleTrafficOrFail(t, revisionedInstance)
}

// sendSimpleTrafficOrFail sends an echo call to the upgrading echo instance
func sendSimpleTrafficOrFail(ctx *testing.T, i echo.Instance) {
	ctx.Helper()
	resp, err := apps.Latest[0].Call(echo.CallOptions{
		Target:   i,
		PortName: "http",
	})
	if resp.CheckOK() != nil {
		ctx.Fatalf("error in call: %v", err)
	}
}

// enableDefaultInjection takes a namespaces and relabels it such that it will have a default sidecar injected
func enableDefaultInjection(ns namespace.Instance) error {
	var errs *multierror.Error
	errs = multierror.Append(errs, ns.SetLabel("istio-injection", "enabled"))
	errs = multierror.Append(errs, ns.RemoveLabel("istio.io/rev"))
	return errs.ErrorOrNil()
}

