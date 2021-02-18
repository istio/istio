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

package pilot

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/pkg/log"
)

// TestRevisionedUpgrade tests a revision-based upgrade from the specified versions to current master
func TestRevisionedUpgrade(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleCluster().
		Features("installation.upgrade").
		Run(func(ctx framework.TestContext) {
			versions := []string{"1.8.0"}
			for _, v := range versions {
				ctx.NewSubTest(fmt.Sprintf("%s->master", v)).Run(func(ctx framework.TestContext) {
					testUpgradeFromVersion(ctx, v)
				})
			}
		})
}

// testUpgradeFromVersion tests an upgrade from the target version to the master version
// provided fromVersion must be present in tests/integration/pilot/testdata/upgrade for the installation to succeed
// TODO(monkeyanator) pass this a generic UpgradeFunc allowing for reuse across in-place and revisioned upgrades
func testUpgradeFromVersion(ctx framework.TestContext, fromVersion string) {
	configs := make(map[string]string)
	ctx.ConditionalCleanup(func() {
		for _, config := range configs {
			_ = ctx.Config().DeleteYAML("istio-system", config)
		}
	})

	// install control plane on the specified version and create namespace pointed to that control plane
	installRevisionOrFail(ctx, fromVersion, configs)
	revision := strings.ReplaceAll(fromVersion, ".", "-")
	revisionedNamespace := namespace.NewOrFail(ctx, ctx, namespace.Config{
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
	builder.BuildOrFail(ctx)
	sendSimpleTrafficOrFail(ctx, revisionedInstance)

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
			if strings.Contains(c.Image, fromVersion) {
				ctx.Fatalf("expected post-upgrade container image not to include %q, got %s", fromVersion, c.Image)
			}
		}
	}

	sendSimpleTrafficOrFail(ctx, revisionedInstance)
}

// sendSimpleTrafficOrFail sends an echo call to the upgrading echo instance
func sendSimpleTrafficOrFail(ctx framework.TestContext, i echo.Instance) {
	ctx.Helper()
	apps.PodA[0].CallWithRetryOrFail(ctx, echo.CallOptions{
		Target:   i,
		PortName: "http",
	})
}

// enableDefaultInjection takes a namespaces and relabels it such that it will have a default sidecar injected
func enableDefaultInjection(ns namespace.Instance) error {
	var errs *multierror.Error
	errs = multierror.Append(errs, ns.SetLabel("istio-injection", "enabled"))
	errs = multierror.Append(errs, ns.RemoveLabel("istio.io/rev"))
	return errs.ErrorOrNil()
}
