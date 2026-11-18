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

package helmupgrade

import (
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/versions"
	helmtest "istio.io/istio/tests/integration/helm"
)

// previousVersion returns the newest published minor older than this checkout's VERSION, and
// nMinusTwoVersion the one before that, as the plain version strings the helm helpers take.
//
// The versions package memoizes the lookup, so calling these per test rather than resolving once
// in a suite Setup still probes the registry at most once. See its package comment for why a
// release branch resolves to its own predecessors rather than the newest minors Istio published.
func previousVersion(t *testing.T) string {
	t.Helper()
	return versions.PreviousMinorOrFail(t, 1).String()
}

func nMinusTwoVersion(t *testing.T) string {
	t.Helper()
	return versions.PreviousMinorOrFail(t, 2).String()
}

// TestDefaultInPlaceUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestDefaultInPlaceUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performInPlaceUpgradeFunc(previousVersion(t), false))
}

// TestCanaryUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestCanaryUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performCanaryUpgradeFunc(helmtest.DefaultNamespaceConfig, previousVersion(t)))
}

// TestCanaryUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestCanaryUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performCanaryUpgradeFunc(helmtest.DefaultNamespaceConfig, nMinusTwoVersion(t)))
}

// TestStableRevisionLabelsUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestStableRevisionLabelsUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performRevisionTagsUpgradeFunc(previousVersion(t)))
}

// TestStableRevisionLabelsUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestStableRevisionLabelsUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performRevisionTagsUpgradeFunc(nMinusTwoVersion(t)))
}

// TestAmbientInPlaceUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with ambient profile for Istio 1.(n-1)
func TestAmbientInPlaceUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performInPlaceUpgradeFunc(previousVersion(t), true))
}

// TestAmbientInPlaceUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with ambient profile for Istio 1.(n-1)
func TestZtunnelFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(upgradeAllButZtunnel(previousVersion(t)))
}

// TestInPlaceUpgradeWebhookFailurePolicy verifies that webhook failurePolicy is set correctly
// after an in-place helm upgrade when validationFailurePolicy is explicitly configured.
func TestInPlaceUpgradeWebhookFailurePolicy(t *testing.T) {
	framework.
		NewTest(t).
		Run(performInPlaceUpgradeWithFailurePolicy(previousVersion(t)))
}

func TestAmbientStableRevisionLabelsGatewayStatus(t *testing.T) {
	framework.
		NewTest(t).
		RequireKubernetesMinorVersion(31).
		Run(runMultipleTagsFunc(true, true))
}
