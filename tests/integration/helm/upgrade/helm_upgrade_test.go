//go:build integ
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

package helmupgrade

import (
	"istio.io/istio/pkg/test/versions"
	"testing"

	"istio.io/istio/pkg/test/framework"
)

// TestDefaultInPlaceUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestDefaultInPlaceUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performInPlaceUpgradeFunc(versions.Previous(1)))
}

// TestCanaryUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestCanaryUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performCanaryUpgradeFunc(versions.Previous(1)))
}

// TestCanaryUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestCanaryUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performCanaryUpgradeFunc(versions.Previous(2)))
}

// TestStableRevisionLabelsUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestStableRevisionLabelsUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performRevisionTagsUpgradeFunc(versions.Previous(1)))
}

// TestStableRevisionLabelsUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestStableRevisionLabelsUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performRevisionTagsUpgradeFunc(versions.Previous(2)))
}
