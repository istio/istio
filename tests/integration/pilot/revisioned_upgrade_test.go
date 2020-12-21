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
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/namespace"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/shell"
	"istio.io/istio/pkg/test/util/retry"
)

// TestRevisionedUpgrade tests a revision-based upgrade from the specified versions to current master
func TestRevisionedUpgrade(t *testing.T) {
	framework.NewTest(t).
		RequiresSingleCluster().
		Features("installation.upgrade").
		Run(func(ctx framework.TestContext) {
			versions := []string{"1.8.0"}
			for _, v := range versions {
				t.Run(fmt.Sprintf("%s->master", v), func(t *testing.T) {
					testUpgradeFromVersion(ctx, t, v)
				})
			}
		})
}

// testUpgradeFromVersion tests an upgrade from the target version to the master version
// provided fromVersion must be present in tests/integration/pilot/testdata/upgrade for the installation to succeed
// TODO(monkeyanator) pass this a generic UpgradeFunc allowing for reuse across in-place and revisioned upgrades
func testUpgradeFromVersion(ctx framework.TestContext, t *testing.T, fromVersion string) {
	configs := make(map[string]string)
	ctx.WhenDone(func() error {
		var errs *multierror.Error
		for _, config := range configs {
			ctx.Config().DeleteYAML("istio-system", config)
			multierror.Append(errs, ctx.Config().DeleteYAML("istio-system", config))
		}
		return errs.ErrorOrNil()
	})

	// install control plane on the specified version and create namespace pointed to that control plane
	installRevisionOrFail(ctx, t, fromVersion, configs)
	revision := strings.ReplaceAll(fromVersion, ".", "-")
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
	sendUpgradeInstanceTrafficOrFail(t, revisionedInstance)

	if err := enableDefaultInjection(revisionedNamespace); err != nil {
		t.Fatalf("could not relabel namespace to enable default injection: %v", err)
	}
	if err := restartNamespaceDeployments(revisionedNamespace.Name()); err != nil {
		t.Fatalf("failed to rollout deployments in namespace %s: %v", revisionedNamespace.Name(), err)
	}

	// ensure that rollout completes with pods running updated proxies
	retry.UntilSuccessOrFail(t, func() error {
		fmt.Println("upgrade retry in progress...")
		fetch := kubetest.NewPodMustFetch(ctx.Clusters().Default(), revisionedInstance.Config().Namespace.Name(), fmt.Sprintf("app=%s", revisionedInstance.Config().Service)) // nolint: lll
		pods, _ := kubetest.CheckPodsAreReady(fetch)

		if len(pods) >= 1 {
			return fmt.Errorf("rollout in progress, multiple instances exist")
		}

		// all containers must be with tag "latest" for rollout to be considered finished
		for _, p := range pods {
			for _, c := range p.Spec.Containers {
				if !strings.Contains(c.Image, ":latest") {
					return fmt.Errorf("rollout in progress, pods not updated to latest")
				}
			}
		}

		return nil
	}, retry.Delay(time.Second*2), retry.Timeout(time.Second*30))
	sendUpgradeInstanceTrafficOrFail(t, revisionedInstance)
}

// sendUpgradeInstanceTrafficOrFail sends an echo call to the upgrading echo instance
func sendUpgradeInstanceTrafficOrFail(t *testing.T, i echo.Instance) {
	t.Helper()
	resp, err := apps.PodA[0].Call(echo.CallOptions{
		Target:   i,
		PortName: "http",
	})
	if resp.CheckOK() != nil {
		t.Fatalf("error in call: %v", err)
	}
}

// enableDefaultInjection takes a namespaces and relabels it such that it will have a default sidecar injected
func enableDefaultInjection(ns namespace.Instance) error {
	var errs *multierror.Error
	errs = multierror.Append(errs, ns.SetLabel("istio-injection", "enabled"))
	errs = multierror.Append(errs, ns.RemoveLabel("istio.io/rev"))
	return errs.ErrorOrNil()
}

// restartNamespaceDeployments performs a `kubectl rollout restart` on the provided namespace
func restartNamespaceDeployments(namespace string) error {
	execCmd := fmt.Sprintf("kubectl rollout restart deployment -n %s", namespace)
	_, err := shell.Execute(true, execCmd)
	return err
}
