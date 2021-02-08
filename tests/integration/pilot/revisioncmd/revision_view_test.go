// +build integ
//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package revisioncmd

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/label"
	"istio.io/istio/istioctl/cmd"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/echoboot"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

var (
	manifestPath = filepath.Join(env.IstioSrc, "manifests")

	componentMap = map[string]string{
		"istiod":          "Pilot",
		"ingress-gateway": "IngressGateways",
		"egress-gateway":  "EgressGateways",
	}

	expectedComponentsPerRevision = map[string]map[string]bool{
		"stable": {
			"base":                          true,
			"istiod":                        true,
			"ingress:istio-ingressgateway":  true,
			"ingress:istio-eastwestgateway": true,
			"egress:istio-egressgateway":    true,
		},
		"canary": {
			"istiod":                        true,
			"ingress:istio-ingressgateway":  true,
			"ingress:istio-eastwestgateway": true,
			"egress:istio-egressgateway":    true,
		},
	}
)

type revisionResource struct {
	ns   namespace.Instance
	echo echo.Instance
}

func TestRevisionCommand(t *testing.T) {
	framework.
		NewTest(t).
		RequiresSingleCluster().
		Features("installation.istioctl.revision_centric_view").
		Run(func(ctx framework.TestContext) {
			skipIfUnsupportedKubernetesVersion(ctx)
			istioCtl := istioctl.NewOrFail(ctx, ctx, istioctl.Config{})

			revisions := []string{"stable", "canary"}
			revResourceMap := map[string]*revisionResource{}
			builder := echoboot.NewBuilder(ctx)
			for _, rev := range revisions {
				effectiveRev := rev
				if rev == "default" {
					effectiveRev = ""
				}
				revResourceMap[rev] = &revisionResource{
					ns: namespace.NewOrFail(t, ctx, namespace.Config{
						Prefix:   fmt.Sprintf("istioctl-rev-%s", rev),
						Inject:   true,
						Revision: effectiveRev,
					}),
				}
				builder.With(&revResourceMap[rev].echo,
					echo.Config{Namespace: revResourceMap[rev].ns})
			}
			builder.BuildOrFail(ctx)

			// Wait some time for things to settle down. This is very
			// important for gateway pod related tests.
			time.Sleep(30 * time.Second)

			testCases := []struct {
				name     string
				testFunc func(framework.TestContext, istioctl.Instance, map[string]*revisionResource)
			}{
				{
					name:     "list revisions",
					testFunc: testRevisionListingVerbose,
				},
				{
					name:     "describe valid revision",
					testFunc: testRevisionDescription,
				},
				{
					name:     "describe non-existent revision",
					testFunc: testNonExistentRevisionDescription,
				},
				{
					name:     "describe invalid revision",
					testFunc: testInvalidRevisionFormat,
				},
				{
					name:     "invalid output format",
					testFunc: testInvalidOutputFormat,
				},
			}
			for _, testCase := range testCases {
				ctx.NewSubTest(testCase.name).Run(func(sctx framework.TestContext) {
					testCase.testFunc(sctx, istioCtl, revResourceMap)
				})
			}
		})
}

func testRevisionListingVerbose(ctx framework.TestContext, istioCtl istioctl.Instance, _ map[string]*revisionResource) {
	listVerboseCmd := []string{"x", "revision", "list", "-d", manifestPath, "-v", "-o", "json"}
	stdout, _, err := istioCtl.Invoke(listVerboseCmd)
	if err != nil {
		ctx.Fatalf("unexpected error while invoking istioctl command %v: %v", listVerboseCmd, err)
	}
	var revDescriptions map[string]*cmd.RevisionDescription
	if err = json.Unmarshal([]byte(stdout), &revDescriptions); err != nil {
		ctx.Fatalf("error while unmarshaling JSON output: %v", err)
	}

	expectedRevisionSet := map[string]bool{"stable": true, "canary": true}
	actualRevisionSet := map[string]bool{}
	for rev := range revDescriptions {
		actualRevisionSet[rev] = true
	}
	if !subsetOf(expectedRevisionSet, actualRevisionSet) {
		ctx.Fatalf("Some expected revisions are missing from actual. "+
			"Expected should be subset of actual. Expected: %v, Actual: %v",
			expectedRevisionSet, actualRevisionSet)
	}

	for rev, descr := range revDescriptions {
		if !expectedRevisionSet[rev] {
			ctx.Logf("revision %s is unexpected. Might be a leftover from some other test. skipping...", rev)
			continue
		}
		verifyRevisionOutput(ctx, descr, rev)
	}
}

func testRevisionDescription(ctx framework.TestContext, istioCtl istioctl.Instance, revResources map[string]*revisionResource) {
	for _, rev := range []string{"stable", "canary"} {
		descr, err := getDescriptionForRevision(istioCtl, rev)
		if err != nil || descr == nil {
			ctx.Fatalf("failed to retrieve description for %s: %v", descr, err)
		}
		verifyRevisionOutput(ctx, descr, rev)
		if resources := revResources[rev]; resources != nil {
			nsName := resources.ns.Name()
			podsInNamespace := []*cmd.PodFilteredInfo{}
			if nsi := descr.NamespaceSummary[nsName]; nsi != nil {
				podsInNamespace = nsi.Pods
			}
			labelSelector, err := meta_v1.LabelSelectorAsSelector(&meta_v1.LabelSelector{
				MatchLabels: map[string]string{label.IoIstioRev.Name: rev},
			})
			if err != nil {
				ctx.Fatalf("error while creating label selector for pods in namespace: %s, revision: %s",
					nsName, rev)
			}
			podsForRev, err := ctx.Clusters().Default().
				CoreV1().Pods(nsName).
				List(context.Background(), meta_v1.ListOptions{LabelSelector: labelSelector.String()})
			if podsForRev == nil || err != nil {
				ctx.Fatalf("error while getting pods for revision: %s from namespace: %s: %v", rev, nsName, err)
			}
			expectedPodsForRev := map[string]bool{}
			actualPodsForRev := map[string]bool{}
			//nolint:staticcheck
			for _, pod := range podsForRev.Items {
				expectedPodsForRev[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)] = true
			}
			for _, pod := range podsInNamespace {
				actualPodsForRev[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)] = true
			}
			if !setsMatch(expectedPodsForRev, actualPodsForRev) {
				ctx.Fatalf("list of pods pointing to %s don't match in namespace %s. Expected: %v, Actual: %v",
					rev, nsName, expectedPodsForRev, actualPodsForRev)
			}
		}
	}
}

func testNonExistentRevisionDescription(ctx framework.TestContext, istioCtl istioctl.Instance, _ map[string]*revisionResource) {
	describeArgs := []string{"x", "revision", "describe", "ghost", "-o", "json", "-v"}
	_, _, err := istioCtl.Invoke(describeArgs)
	if err == nil {
		ctx.Fatalf("expected error for non-existent revision 'ghost', but didn't get it")
	}
}

func testInvalidRevisionFormat(ctx framework.TestContext, istioCtl istioctl.Instance, _ map[string]*revisionResource) {
	describeArgs := []string{"x", "revision", "describe", "$%#@abc", "-o", "json", "-v"}
	_, _, err := istioCtl.Invoke(describeArgs)
	if err == nil || !strings.Contains(err.Error(), "invalid revision format") {
		ctx.Fatalf("expected error message saying revision format is invalid, but got %v", err)
	}
}

func testInvalidOutputFormat(ctx framework.TestContext, istioCtl istioctl.Instance, _ map[string]*revisionResource) {
	type invalidFormatTest struct {
		name    string
		command []string
	}
	for _, t := range []invalidFormatTest{
		{name: "list", command: []string{"x", "revision", "list", "-o", "mystery"}},
		{name: "describe", command: []string{"x", "revision", "describe", "canary", "-o", "mystery"}},
	} {
		ctx.NewSubTest(t.name).Run(func(sctx framework.TestContext) {
			_, _, fErr := istioCtl.Invoke(t.command)
			if fErr == nil {
				ctx.Fatalf("expected error due to invalid format, but got nil")
			}
		})
	}
}

func getDescriptionForRevision(istioCtl istioctl.Instance, revision string) (*cmd.RevisionDescription, error) {
	describeCmd := []string{"x", "revision", "describe", revision, "-d", manifestPath, "-o", "json", "-v"}
	descr, _, err := istioCtl.Invoke(describeCmd)
	if err != nil {
		return nil, err
	}
	var revDescription cmd.RevisionDescription
	if err = json.Unmarshal([]byte(descr), &revDescription); err != nil {
		return nil, fmt.Errorf("error while unmarshaling revision description JSON for"+
			" revision=%s : %v", revision, err)
	}
	return &revDescription, nil
}

func verifyRevisionOutput(ctx framework.TestContext, descr *cmd.RevisionDescription, rev string) {
	expectedTagSet := map[string]bool{}
	actualTagSet := map[string]bool{}
	for _, mwh := range descr.Webhooks {
		if mwh.Tag != "" {
			actualTagSet[mwh.Tag] = true
		}
	}
	if !setsMatch(expectedTagSet, actualTagSet) {
		ctx.Fatalf("tag sets don't match for %s. Expected: %v, Actual:%v", rev, expectedTagSet, actualTagSet)
	}
	expectedComponents, ok := expectedComponentsPerRevision[rev]
	if !ok {
		ctx.Fatalf("unexpected error. Could not find expected components for %s", rev)
	}
	actualComponents := map[string]bool{}
	for _, iop := range descr.IstioOperatorCRs {
		for _, comp := range iop.Components {
			actualComponents[comp] = true
		}
	}
	if !setsMatch(expectedComponents, actualComponents) {
		ctx.Fatalf("required component sets don't match for revision %s. Expected: %v, Actual: %v",
			rev, expectedComponents, actualComponents)
	}
	verifyComponentPodsForRevision(ctx, "istiod", rev, descr)
	verifyComponentPodsForRevision(ctx, "ingress-gateway", rev, descr)
	verifyComponentPodsForRevision(ctx, "egress-gateway", rev, descr)
}

func verifyComponentPodsForRevision(ctx framework.TestContext, component, rev string, descr *cmd.RevisionDescription) {
	opComponent := componentMap[component]
	if opComponent == "" {
		ctx.Fatalf("unknown component: %s", component)
	}
	labelSelector, err := meta_v1.LabelSelectorAsSelector(&meta_v1.LabelSelector{
		MatchLabels: map[string]string{
			label.IoIstioRev.Name:        rev,
			label.OperatorComponent.Name: opComponent,
		},
	})
	if err != nil {
		ctx.Fatalf("unexpected error: failed to create label selector: %v", err)
	}
	componentPods, err := ctx.Clusters().Default().
		CoreV1().Pods("").
		List(context.Background(), meta_v1.ListOptions{LabelSelector: labelSelector.String()})
	if err != nil {
		ctx.Fatalf("unexpected error while fetching %s pods for revision %s: %v", component, rev, err)
	}
	expectedComponentPodSet := map[string]bool{}
	for _, pod := range componentPods.Items {
		podName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		expectedComponentPodSet[podName] = true
	}

	actualComponentPodSet := map[string]bool{}
	podList := []*cmd.PodFilteredInfo{}
	switch component {
	case "istiod":
		podList = descr.ControlPlanePods
	case "ingress-gateway":
		podList = descr.IngressGatewayPods
	case "egress-gateway":
		podList = descr.EgressGatewayPods
	}
	for _, pod := range podList {
		podName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
		actualComponentPodSet[podName] = true
	}

	if !setsMatch(expectedComponentPodSet, actualComponentPodSet) {
		ctx.Fatalf("%s pods are not listed properly. Expected: %v, Actual: %v",
			component, expectedComponentPodSet, actualComponentPodSet)
	}
}

func setsMatch(expected map[string]bool, actual map[string]bool) bool {
	if len(expected) != len(actual) {
		return false
	}
	for x := range actual {
		if !expected[x] {
			return false
		}
	}
	return true
}

func subsetOf(a map[string]bool, b map[string]bool) bool {
	for ax := range a {
		if !b[ax] {
			return false
		}
	}
	return true
}

// MutatingWebhookConfiguration version used in this feature is v1 which is only
// supported from Kubernetes 1.16. For Kubernetes 1.15, we should be using v1beta1.
// However, this feature is not available only in Istio versions later than 1.8
// don't support Kubernetes 1.15. So we are fine. Not having this caused postsubmit
// test flakes when run against 1.15
//
// K8s 1.15 - https://v1-16.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.15/#mutatingwebhookconfiguration-v1beta1-admissionregistration-k8s-io
// K8s 1.16 - https://v1-16.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.16/#mutatingwebhookconfiguration-v1-admissionregistration-k8s-io
func skipIfUnsupportedKubernetesVersion(ctx framework.TestContext) {
	if !ctx.Clusters().Default().MinKubeVersion(1, 16) {
		ctx.Skipf("k8s version not supported for %s (<%s)", ctx.Name(), "1.16")
	}
}
