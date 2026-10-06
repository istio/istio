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

// Package versions resolves the released Istio versions that skew tests should run against,
// derived from the VERSION file rather than hardcoded. This keeps the tested versions moving
// forward automatically as releases are cut.
//
// Everything here is relative to the VERSION file of the checkout the tests run from, not to what
// Istio has published globally. On master that is the version under development, so the resolved
// versions are the newest releases. On a release branch it is that branch's own version, so the
// resolved versions are the ones that branch is expected to interoperate with: running from
// release-1.30 yields 1.29, 1.28, ... even though newer minors exist upstream. That is the
// intended behavior - a release branch should be tested against the versions it shipped
// alongside, not against minors released after it.
package versions

import (
	"fmt"

	"github.com/Masterminds/semver/v3"

	"istio.io/istio/pkg/lazy"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/util/image"
)

// imageToCheck is the image we probe to find out whether a minor version has been released yet.
//
// It has to live on Docker Hub, the only registry that has every release. Since 1.31 it is the
// only place releases are published to, and it also has the older releases that
// registry.istio.io/release and gcr.io/istio-release mirrored. Probing those instead would report
// every 1.31+ release as missing, so the tests would quietly run one minor version further back
// than their names say.
const imageToCheck = "docker.io/istio/pilot"

// imageExists is overridable so unit tests can avoid reaching out to the registry.
var imageExists = func(v *semver.Version) (bool, error) {
	return image.Exists(imageToCheck + ":" + v.String())
}

// previousPublishedMinor resolves the most recent published minor older than the VERSION file of
// this checkout. It is usually the minor immediately before it, but on master early in a release
// cycle that one is not published yet, so it may be two behind. On a release branch the preceding
// minor is always published, so the fallback never triggers there.
//
// Note this is not necessarily the newest minor Istio has published: on release-1.30 it resolves
// to 1.29 regardless of how many minors have shipped since. See the package comment.
var previousPublishedMinor = lazy.NewWithRetry(func() (*semver.Version, error) {
	versionFromFile, err := env.ReadVersion()
	if err != nil {
		return nil, err
	}

	v, err := semver.NewVersion(versionFromFile)
	if err != nil {
		return nil, err
	}

	previousVersion, err := minorBefore(v, 1)
	if err != nil {
		return nil, err
	}

	// If the previous minor is not published yet, fall back to the one before it.
	if exists, err := imageExists(previousVersion); err != nil {
		return nil, err
	} else if !exists {
		if previousVersion, err = minorBefore(v, 2); err != nil {
			return nil, err
		}
	}

	return previousVersion, nil
})

// PreviousMinor returns the Nth published minor version preceding the one in the VERSION file,
// counting back from the newest. PreviousMinor(1) is the minor before this checkout's version,
// PreviousMinor(2) the one before that, and so on. From master with VERSION 1.32 that is 1.31,
// 1.30, ...; from release-1.30 it is 1.29, 1.28, ...
//
// The patch of the returned version is always zero: only the minor line is meaningful here, so
// callers resolving charts or images should treat it as a minor-version constraint rather than an
// exact release, and match images on the "<major>.<minor>." prefix.
func PreviousMinor(n int) (*semver.Version, error) {
	if n < 1 {
		return nil, fmt.Errorf("previous minor version index must be at least 1, got %d", n)
	}
	v, err := previousPublishedMinor.Get()
	if err != nil {
		return nil, err
	}
	return minorBefore(v, uint64(n-1))
}

// PreviousMinorOrFail is PreviousMinor, failing the test instead of returning an error.
func PreviousMinorOrFail(t test.Failer, n int) *semver.Version {
	t.Helper()
	v, err := PreviousMinor(n)
	if err != nil {
		t.Fatalf("failed to resolve previous Istio minor version: %v", err)
	}
	return v
}

// MinorPrefix returns the "<major>.<minor>." image-tag prefix of v.
//
// Charts are installed with a "~" constraint and tag their images with their own version, so
// "~1.31.0" installs chart 1.31.1 and its pods run images tagged 1.31.1. Matching an image against
// the full version finds nothing; this prefix is what callers mean by "from the 1.31 release line".
func MinorPrefix(v *semver.Version) string {
	return fmt.Sprintf("%d.%d.", v.Major(), v.Minor())
}

// minorBefore returns the first release (x.y.0) of the minor version `back` minors before v.
// For example, minorBefore(1.33.4, 2) returns 1.31.0.
//
// The patch is reset to 0 and any prerelease or build metadata is dropped, because those describe
// v, not the older minor. Carrying them over would invent a version that was never published:
// 1.32.4 does not exist if the 1.32 line ended at 1.32.1, and 1.32.0-dev was never a release at
// all. The existence check in previousPublishedMinor would then report a minor that does exist as
// missing. Every published minor has an x.y.0, so that is what we ask for.
func minorBefore(v *semver.Version, back uint64) (*semver.Version, error) {
	if back > v.Minor() {
		return nil, fmt.Errorf("cannot go back %d minor versions from %s", back, v)
	}
	return semver.New(v.Major(), v.Minor()-back, 0, "", ""), nil
}
