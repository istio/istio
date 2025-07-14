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

const imageToCheck = "gcr.io/istio-release/pilot"

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
	previousVersion := semver.New(v.Major(), v.Minor()-1, v.Patch(), v.Prerelease(), v.Metadata())

	// If the previous version is not published yet, use the latest one
	if exists, err := image.Exists(imageToCheck + ":" + previousVersion.String()); err != nil {
		return err
	} else if !exists {
		previousVersion = semver.New(v.Major(), v.Minor()-2, v.Patch(), v.Prerelease(), v.Metadata())
	}

	previousSupportedVersion = previousVersion.String()
	nMinusTwoVersion = semver.New(previousVersion.Major(), previousVersion.Minor()-1, previousVersion.Patch(),
		previousVersion.Prerelease(), previousVersion.Metadata()).String()

	return nil
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
		Run(performRevisionTagsUpgradeFunc(previousSupportedVersion, false, false))
}

// TestStableRevisionLabelsUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestStableRevisionLabelsUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performRevisionTagsUpgradeFunc(nMinusTwoVersion, false, false))
}

// TestAmbientStableRevisionLabelsUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestAmbientStableRevisionLabelsUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performRevisionTagsUpgradeFunc(previousSupportedVersion, true, false))
}

// TestStableRevisionLabelsUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestAmbientStableRevisionLabelsUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Run(performRevisionTagsUpgradeFunc(nMinusTwoVersion, true, false))
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

func TestAmbientStableRevisionLabelsGatewayStatus(t *testing.T) {
	framework.
		NewTest(t).
		Run(runMultipleTagsFunc(true, true))
}
