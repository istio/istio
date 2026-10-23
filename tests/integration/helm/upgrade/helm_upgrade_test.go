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
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/util/image"
	helmtest "istio.io/istio/tests/integration/helm"
)

var (
	currentVersion           string
	previousSupportedVersion string
	nMinusTwoVersion         string
)

// imageToCheck is the image we probe to find out whether a minor version has been released yet.
//
// It has to live on Docker Hub, the only registry that has every release. Since 1.31 it is the
// only place releases are published to, and it also has the older releases that
// registry.istio.io/release mirrored. Probing registry.istio.io/release instead would report
// every 1.31+ release as missing, so the tests would quietly run one minor version further back
// than their names say.
const imageToCheck = "docker.io/istio/pilot"

func initVersions(ctx resource.Context) error {
	versionFromFile, err := env.ReadVersion()
	if err != nil {
		return err
	}

	v, err := semver.NewVersion(versionFromFile)
	if err != nil {
		return err
	}

	currentVersion = v.String()
	previousVersion, err := previousMinor(v, 1)
	if err != nil {
		return err
	}

	// If the previous version is not published yet, use the latest one
	if exists, err := image.Exists(imageToCheck + ":" + previousVersion.String()); err != nil {
		return err
	} else if !exists {
		if previousVersion, err = previousMinor(v, 2); err != nil {
			return err
		}
	}

	nMinusTwo, err := previousMinor(previousVersion, 1)
	if err != nil {
		return err
	}

	previousSupportedVersion = previousVersion.String()
	nMinusTwoVersion = nMinusTwo.String()

	return nil
}

// previousMinor returns the first release (x.y.0) of the minor version `back` minors before v.
// For example, previousMinor(1.33.4, 2) returns 1.31.0.
//
// The patch is reset to 0 and any prerelease or build metadata is dropped, because those describe
// v, not the older minor. A 1.32.4 need not exist if the 1.32 line stopped at 1.32.1, and a tag
// like 1.32.0-dev was never released at all; either one would make the release check above treat
// a minor that does exist as missing. x.y.0 is the one release every published minor has.
func previousMinor(v *semver.Version, back uint64) (*semver.Version, error) {
	if back > v.Minor() {
		return nil, fmt.Errorf("cannot go back %d minor versions from %s", back, v)
	}
	return semver.New(v.Major(), v.Minor()-back, 0, "", ""), nil
}

// TestDefaultInPlaceUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestDefaultInPlaceUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performInPlaceUpgradeFunc(previousSupportedVersion, false))
}

// TestCanaryUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestCanaryUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performCanaryUpgradeFunc(helmtest.DefaultNamespaceConfig, previousSupportedVersion))
}

// TestCanaryUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestCanaryUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performCanaryUpgradeFunc(helmtest.DefaultNamespaceConfig, nMinusTwoVersion))
}

// TestStableRevisionLabelsUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestStableRevisionLabelsUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performRevisionTagsUpgradeFunc(previousSupportedVersion))
}

// TestStableRevisionLabelsUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestStableRevisionLabelsUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performRevisionTagsUpgradeFunc(nMinusTwoVersion))
}

// TestAmbientInPlaceUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with ambient profile for Istio 1.(n-1)
func TestAmbientInPlaceUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performInPlaceUpgradeFunc(previousSupportedVersion, true))
}

// TestAmbientInPlaceUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with ambient profile for Istio 1.(n-1)
func TestZtunnelFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(upgradeAllButZtunnel(previousSupportedVersion))
}

// TestInPlaceUpgradeWebhookFailurePolicy verifies that webhook failurePolicy is set correctly
// after an in-place helm upgrade when validationFailurePolicy is explicitly configured.
func TestInPlaceUpgradeWebhookFailurePolicy(t *testing.T) {
	framework.
		NewTest(t).
		Run(performInPlaceUpgradeWithFailurePolicy(previousSupportedVersion))
}

func TestAmbientStableRevisionLabelsGatewayStatus(t *testing.T) {
	framework.
		NewTest(t).
		RequireKubernetesMinorVersion(31).
		Run(runMultipleTagsFunc(true, true))
}
