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

package pilot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/util/traffic"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	kubetest "istio.io/istio/pkg/test/kube"
)

const (
	callInterval     = 200 * time.Millisecond
	successThreshold = 0.95
)

// TestRevisionedUpgrade tests a revision-based upgrade from the specified versions to current master
func TestRevisionedUpgrade(t *testing.T) {
	// nolint: staticcheck
	framework.NewTest(t).
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		// Requires installation of CPs from manifests, won't succeed
		// if existing CPs have different root cert
		Label(label.CustomSetup).
		Run(func(t framework.TestContext) {
			t.Skip("https://github.com/istio/istio/pull/46213")
			// Kubernetes 1.22 drops support for a number of legacy resources, so we cannot install the old versions
			if !t.Clusters().Default().MaxKubeVersion(21) {
				t.Skipf("k8s version not supported for %s (>%s)", t.Name(), "1.21")
			}
			versions := []string{NMinusOne, NMinusTwo, NMinusThree, NMinusFour}
			for _, v := range versions {
				t.NewSubTest(fmt.Sprintf("%s->master", v)).Run(func(t framework.TestContext) {
					testUpgradeFromVersion(t, v)
				})
			}
		})
}

// testUpgradeFromVersion tests an upgrade from the target version to the master version
// provided fromVersion must be present in tests/integration/pilot/testdata/upgrade for the installation to succeed
// TODO(monkeyanator) pass this a generic UpgradeFunc allowing for reuse across in-place and revisioned upgrades
func testUpgradeFromVersion(t framework.TestContext, fromVersion string) {
	configs := make(map[string]string)
	t.CleanupConditionally(func() {
		for _, config := range configs {
			_ = t.ConfigIstio().YAML("istio-system", config).Delete()
		}
	})

	// install control plane on the specified version and create namespace pointed to that control plane
	installRevisionOrFail(t, fromVersion, configs)
	revision := strings.ReplaceAll(fromVersion, ".", "-")
	revisionedNamespace := namespace.NewOrFail(t, namespace.Config{
		Prefix:   revision,
		Inject:   true,
		Revision: revision,
	})

	var revisionedInstance echo.Instance
	builder := deployment.New(t)
	builder.With(&revisionedInstance, echo.Config{
		Service:   fmt.Sprintf("svc-%s", revision),
		Namespace: revisionedNamespace,
		Ports: []echo.Port{
			{
				Name:         "http",
				Protocol:     protocol.HTTP,
				WorkloadPort: 8080,
			},
		},
	})
	builder.BuildOrFail(t)

	// Create a traffic generator between A and B.
	g := traffic.NewGenerator(t, traffic.Config{
		Source: apps.A[0],
		Options: echo.CallOptions{
			To:    apps.B,
			Count: 1,
			Port: echo.Port{
				Name: "http",
			},
		},
		Interval: callInterval,
	}).Start()

	if err := enableDefaultInjection(revisionedNamespace); err != nil {
		t.Fatalf("could not relabel namespace to enable default injection: %v", err)
	}

	log.Infof("rolling out echo workloads for service %q", revisionedInstance.Config().Service)
	if err := revisionedInstance.Restart(); err != nil {
		t.Fatalf("revisioned instance rollout failed with: %v", err)
	}
	fetch := kubetest.NewPodMustFetch(t.Clusters().Default(), revisionedInstance.Config().Namespace.Name(), fmt.Sprintf("app=%s", revisionedInstance.Config().Service)) // nolint: lll
	pods, err := kubetest.CheckPodsAreReady(fetch)
	if err != nil {
		t.Fatalf("failed to retrieve upgraded pods: %v", err)
	}
	for _, p := range pods {
		for _, c := range p.Spec.Containers {
			if strings.Contains(c.Image, fromVersion) {
				t.Fatalf("expected post-upgrade container image not to include %q, got %q", fromVersion, c.Image)
			}
		}
	}

	// Stop the traffic generator and get the result.
	g.Stop().CheckSuccessRate(t, successThreshold)
}

// enableDefaultInjection takes a namespaces and relabels it such that it will have a default sidecar injected
func enableDefaultInjection(ns namespace.Instance) error {
	var errs *multierror.Error
	errs = multierror.Append(errs, ns.SetLabel("istio-injection", "enabled"))
	errs = multierror.Append(errs, ns.RemoveLabel("istio.io/rev"))
	return errs.ErrorOrNil()
}
