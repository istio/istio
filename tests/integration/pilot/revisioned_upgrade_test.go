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

	"github.com/Masterminds/semver/v3"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/test/framework"
	kubecluster "istio.io/istio/pkg/test/framework/components/cluster/kube"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/check"
	"istio.io/istio/pkg/test/framework/components/echo/deployment"
	"istio.io/istio/pkg/test/framework/components/echo/util/traffic"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/helm"
	kubetest "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/versions"
	helmtest "istio.io/istio/tests/integration/helm"
)

const (
	callInterval     = 200 * time.Millisecond
	successThreshold = 0.95

	// previousVersionCount is how many released minor versions to test against.
	previousVersionCount = 3

	// releaseHub is where images for the released versions are published. Docker Hub is the only
	// registry holding both the pre-1.31 releases and the 1.31+ ones, so it works across the whole
	// range we test. The charts still default global.hub to registry.istio.io/release up to 1.30,
	// hence the explicit override.
	releaseHub = "docker.io/istio"
)

// previousVersions returns the released versions to run skew tests against, newest first. These are
// derived from the VERSION file of this checkout, so they move forward on their own as releases are
// cut, and on a release branch they are the minors preceding that branch rather than the newest
// minors Istio has published. See the versions package comment.
func previousVersions(t framework.TestContext) []*semver.Version {
	out := make([]*semver.Version, 0, previousVersionCount)
	for n := 1; n <= previousVersionCount; n++ {
		out = append(out, versions.PreviousMinorOrFail(t, n))
	}
	return out
}

// revisionName converts a version into a name usable as a revision label value.
func revisionName(version *semver.Version) string {
	return strings.ReplaceAll(version.String(), ".", "-")
}

// TestRevisionedUpgrade tests a revision-based upgrade from the specified versions to the suite's
// default control plane. That target is whatever this checkout builds, so it is the development
// version on master and the branch's own version when the test runs from a release branch.
func TestRevisionedUpgrade(t *testing.T) {
	// nolint: staticcheck
	framework.NewFullTest(t).
		RequiresSingleCluster().
		RequiresLocalControlPlane().
		// Installs additional control planes into the suite's istio-system, which only works if they
		// all share its root cert.
		Label(label.CustomSetup).
		Run(func(t framework.TestContext) {
			for _, v := range previousVersions(t) {
				t.NewSubTest(fmt.Sprintf("%s->default", v)).Run(func(t framework.TestContext) {
					testUpgradeFromVersion(t, v)
				})
			}
		})
}

// testUpgradeFromVersion tests an upgrade from the target version to the version this checkout
// builds, which the suite installs as the default (unrevisioned) control plane.
// fromVersion must be a released version, as its control plane is installed from the published charts
func testUpgradeFromVersion(t framework.TestContext, fromVersion *semver.Version) {
	// install control plane on the specified version and create namespace pointed to that control plane
	revision := revisionName(fromVersion)
	installRevisionOrFail(t, fromVersion, revision)
	oldRevNamespace := namespace.NewOrFail(t, namespace.Config{
		Prefix:   revision,
		Inject:   true,
		Revision: revision,
	})

	// Home for the client managed by the default control plane. The suite's shared echo namespace
	// cannot host it: its setup pins a restrict-to-namespace Sidecar whose egress list is fixed at
	// suite start, so a namespace created later in the test is absent from it and traffic to the
	// destination falls through to PassthroughCluster - routed by original destination address
	// rather than as a mesh service, and therefore never mTLS. A namespace created here carries no
	// Sidecar and keeps the default scope.
	// cross_revision_test.go hits the same wall and solves it the other way, overriding the scope with
	// an egress "*/*" Sidecar that selects the shared app. That saves a namespace and a deployment, at
	// the cost of widening the config of a workload every other test in the suite shares - here for as
	// long as this test drives it under load across a rolling restart.
	defaultNamespace := namespace.NewOrFail(t, namespace.Config{
		Prefix: fmt.Sprintf("default-%s", revision),
		Inject: true,
	})

	// Three protocols, because the config a control plane generates for each of them diverges. A skew
	// bug can land on one and leave the others working, so testing only HTTP would miss it.
	echoPorts := []echo.Port{
		{
			Name:         "http",
			Protocol:     protocol.HTTP,
			WorkloadPort: 8080,
		},
		{
			Name:         "tcp",
			Protocol:     protocol.TCP,
			WorkloadPort: 9000,
		},
		{
			Name:         "grpc",
			Protocol:     protocol.GRPC,
			WorkloadPort: 9090,
		},
	}

	var revisionedInstance, revisionedClient, defaultClient echo.Instance
	builder := deployment.New(t)
	builder.With(&revisionedInstance, echo.Config{
		Service:   fmt.Sprintf("svc-%s", revision),
		Namespace: oldRevNamespace,
		// More than one replica so the rolling restart below always leaves a ready endpoint behind.
		// With a single replica, the drain window of the only pod can on its own consume the success
		// budget, which would make the check measure pod swap timing rather than cross-revision traffic.
		Subsets: []echo.SubsetConfig{{Replicas: 2}},
		Ports:   echoPorts,
	})
	// A client that deliberately stays behind on the revisioned control plane. Both the sidecar image
	// and the discovery address are fixed at injection time: the address is baked in as a static
	// PROXY_CONFIG env var pointing at istiod-<rev>. Relabelling the namespace rewrites
	// neither, so for as long as this deployment is not restarted its proxy is programmed by the old
	// control plane -- across a reconnect too, since it redials the same address. That is what
	// exercises the skew in the direction where the old control plane generates the outbound config.
	builder.With(&revisionedClient, echo.Config{
		Service:   fmt.Sprintf("cli-%s", revision),
		Namespace: oldRevNamespace,
		Ports:     echoPorts,
	})
	// The opposite skew: injected by the default control plane, so its outbound config is generated by
	// the control plane the destination is about to move onto rather than the one it is moving off.
	builder.With(&defaultClient, echo.Config{
		Service:   "cli-default",
		Namespace: defaultNamespace,
		Ports:     echoPorts,
	})
	builder.BuildOrFail(t)

	// Scoped to this namespace rather than the mesh: the suite's shared namespace holds uninjected
	// workloads that can only speak plaintext, so a root-namespace policy would break tests that have
	// nothing to do with this one. Both control planes watch every namespace, so they both translate
	// it anyway - the old revision programs the destination's inbound until the restart and the
	// default control plane afterwards. Applied before any traffic starts.
	t.ConfigIstio().YAML(oldRevNamespace.Name(), `
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: strict-mtls
spec:
  mtls:
    mode: STRICT`).ApplyOrFail(t)

	// Both paths target the workload that migrates from the revisioned control plane to the default
	// one. Only the source differs: one client is managed by the default control plane, the other by
	// the old revision, so between them they cover a skewed call in each direction as the destination
	// moves across the upgrade.
	callOptions := echo.CallOptions{
		To:    revisionedInstance,
		Count: 1,
		Port: echo.Port{
			Name: "http",
		},
		// A 200 on its own is weak evidence. If a skew bug left one control plane unable to tell that
		// the other's workloads carry a sidecar, the call would downgrade to plaintext; STRICT above
		// makes the destination refuse that, and requiring the forwarded client cert confirms the peer
		// identity reached the application rather than dying at the proxy. The cert check fails open on
		// non-HTTP responses, so the same checker is reusable for the TCP port below.
		Check: check.And(check.OK(), check.MTLSForHTTP()),
	}
	// Same call on each of the other protocols. Only the port differs.
	callOptionsFor := func(port string) echo.CallOptions {
		o := callOptions
		o.Port = echo.Port{Name: port}
		return o
	}

	// Before the upgrade: both clients reach a destination injected by the old revision, on every
	// protocol.
	callAllProtocols := func(t framework.TestContext) {
		t.Helper()
		for _, port := range []string{"http", "tcp", "grpc"} {
			defaultClient.CallOrFail(t, callOptionsFor(port))
			revisionedClient.CallOrFail(t, callOptionsFor(port))
		}
	}
	callAllProtocols(t)

	// During the upgrade: keep both paths under load while the destination is relabelled and rolled
	// onto the default control plane. Neither source is restarted, so a failure recorded here is a
	// mesh failure rather than the test framework losing its connection to the pod it drives.
	// HTTP only. What the success budget measures is whether endpoints stay reachable across the
	// restart, which is not protocol-specific, and running all three would triple the request rate
	// against a two-replica destination mid-rollout - turning the threshold into a load measurement.
	// The protocol-specific config is covered by the one-shot calls on either side of the upgrade.
	fromDefault := traffic.NewGenerator(t, traffic.Config{
		Source:   defaultClient,
		Options:  callOptions,
		Interval: callInterval,
	}).Start()
	fromRevision := traffic.NewGenerator(t, traffic.Config{
		Source:   revisionedClient,
		Options:  callOptions,
		Interval: callInterval,
	}).Start()

	if err := enableDefaultInjection(oldRevNamespace); err != nil {
		t.Fatalf("could not relabel namespace to enable default injection: %v", err)
	}

	log.Infof("rolling out echo workloads for service %q", revisionedInstance.Config().Service)
	if err := revisionedInstance.Restart(); err != nil {
		t.Fatalf("revisioned instance rollout failed with: %v", err)
	}
	// The chart is pinned to a minor version, so the installed patch is whatever was latest. Match on
	// the minor prefix rather than the exact version we asked for. Built from the parsed fields rather
	// than sliced off the string, which would latch onto the wrong dot given a dotted prerelease.
	fromMinorPrefix := versions.MinorPrefix(fromVersion)
	for _, image := range proxyImagesOrFail(t, revisionedInstance) {
		if strings.Contains(image, fromMinorPrefix) {
			t.Fatalf("expected post-upgrade proxy image not to be from the %sx release line, got %q", fromMinorPrefix, image)
		}
	}

	// The client is what makes the fromRevision generator a skew test, and it only stays skewed for as
	// long as nothing restarts it: the namespace relabel above does not touch running pods, and
	// Restart() is scoped to a single deployment. Assert that rather than assume it, so that a change
	// which starts rolling the whole namespace fails here instead of quietly turning both generators
	// into the same unskewed measurement against the default control plane.
	for _, image := range proxyImagesOrFail(t, revisionedClient) {
		if !strings.Contains(image, fromMinorPrefix) {
			t.Fatalf("revisioned client should still run its original %sx proxy, got %q", fromMinorPrefix, image)
		}
	}

	// Stop both generators before asserting on either, so that a failure on the first does not leave
	// the second one running for the rest of the suite.
	fromDefaultResult, fromRevisionResult := fromDefault.Stop(), fromRevision.Stop()
	fromDefaultResult.CheckSuccessRate(t, successThreshold)
	fromRevisionResult.CheckSuccessRate(t, successThreshold)

	// After the upgrade: both clients still reach the destination now that it runs under the default
	// control plane. The revisioned client is still on its original sidecar, so this is the skewed
	// pairing in the direction the default-managed client cannot cover.
	callAllProtocols(t)
}

// proxyImagesOrFail returns the sidecar image of every ready pod backing the given echo instance. The
// sidecar is the only container that tracks the control plane that injected the pod, so it is what the
// upgrade assertions compare against; the application container comes from this checkout either way.
func proxyImagesOrFail(t framework.TestContext, i echo.Instance) []string {
	t.Helper()
	ns, svc := i.Config().Namespace.Name(), i.Config().Service
	pods, err := kubetest.CheckPodsAreReady(kubetest.NewPodMustFetch(t.Clusters().Default(), ns, fmt.Sprintf("app=%s", svc)))
	if err != nil {
		t.Fatalf("failed to retrieve pods for %s/%s: %v", ns, svc, err)
	}
	if len(pods) == 0 {
		t.Fatalf("no ready pods found for %s/%s", ns, svc)
	}
	images := make([]string, 0, len(pods))
	for _, p := range pods {
		image := ""
		// On clusters that support them the sidecar is injected as a native sidecar, which lands in
		// InitContainers rather than Containers, so both lists have to be searched. Looking at only one
		// of them silently finds no proxy at all and makes every assertion below vacuously true.
		for _, c := range append(append([]corev1.Container{}, p.Spec.InitContainers...), p.Spec.Containers...) {
			if c.Name == inject.ProxyContainerName {
				image = c.Image
				break
			}
		}
		if image == "" {
			t.Fatalf("pod %s/%s has no %s container, so it was never injected", ns, p.Name, inject.ProxyContainerName)
		}
		images = append(images, image)
	}
	return images
}

// installRevisionOrFail installs a revisioned control plane running the given released Istio version.
// The chart is pulled from the published Helm repo at test time, so the tested versions follow the
// VERSION file instead of checked-in manifests. Only the istiod chart is installed: CRDs and the
// validating webhook come from the suite's own installation, and every control plane in istio-system
// shares its root CA via the istio-ca-secret.
func installRevisionOrFail(t framework.TestContext, version *semver.Version, revision string) {
	cs := t.Clusters().Default().(*kubecluster.Cluster)
	h := helm.New(cs.Filename())
	release := fmt.Sprintf("%s-%s", helmtest.IstiodReleaseName, revision)
	systemNamespace := i.Settings().SystemNamespace

	t.CleanupConditionally(func() {
		if err := h.DeleteChart(release, systemNamespace); err != nil {
			scopes.Framework.Errorf("failed to delete helm release %s: %v", release, err)
		}
	})

	// The tag is left empty so that it comes from the chart, which carries the one matching the
	// release. Sharing the helm suite's helper keeps these values consistent with how the suite
	// installs its own control plane.
	valuesFile := helmtest.GetValuesOverrides(t, releaseHub, "", t.Settings().Image.Variant, revision, false)

	// Pin to the minor version only, so the latest patch release is always picked up. --wait keeps us
	// from creating injected workloads before the revision's webhook can serve them.
	versionArgs := fmt.Sprintf("--repo %s --version ~%s --wait", t.Settings().HelmRepo, version)
	if err := h.InstallChart(release, helmtest.RepoDiscoveryChartPath, systemNamespace,
		valuesFile, helmtest.Timeout, versionArgs); err != nil {
		t.Fatalf("failed to install revisioned control plane %s: %v", version, err)
	}
}

// enableDefaultInjection takes a namespaces and relabels it such that it will have a default sidecar injected
func enableDefaultInjection(ns namespace.Instance) error {
	var errs *multierror.Error
	errs = multierror.Append(errs, ns.SetLabel("istio-injection", "enabled"))
	errs = multierror.Append(errs, ns.RemoveLabel("istio.io/rev"))
	return errs.ErrorOrNil()
}
