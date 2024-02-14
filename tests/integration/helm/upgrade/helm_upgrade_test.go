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
)

var (
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

	// Workaround publish issues
	if previousSupportedVersion == "1.17.0" {
		previousSupportedVersion = "1.17.1"
	}
	if nMinusTwoVersion == "1.17.0" {
		nMinusTwoVersion = "1.17.1"
	}

	return nil
}

// TestDefaultInPlaceUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestDefaultInPlaceUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performInPlaceUpgradeFunc(previousSupportedVersion, false))
}

// TestCanaryUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestCanaryUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performCanaryUpgradeFunc(previousSupportedVersion))
}

// TestCanaryUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestCanaryUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performCanaryUpgradeFunc(nMinusTwoVersion))
}

// TestStableRevisionLabelsUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-1)
func TestStableRevisionLabelsUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performRevisionTagsUpgradeFunc(previousSupportedVersion))
}

// TestStableRevisionLabelsUpgradeFromTwoMinorRelease tests Istio upgrade using Helm with default options for Istio 1.(n-2)
func TestStableRevisionLabelsUpgradeFromTwoMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.default.upgrade").
		Run(performRevisionTagsUpgradeFunc(nMinusTwoVersion))
}

// TestAmbientInPlaceUpgradeFromPreviousMinorRelease tests Istio upgrade using Helm with ambient profile for Istio 1.(n-1)
func TestAmbientInPlaceUpgradeFromPreviousMinorRelease(t *testing.T) {
	framework.
		NewTest(t).
		Features("installation.helm.ambient.upgrade").
		Run(performInPlaceUpgradeFunc(previousSupportedVersion, true))
}
