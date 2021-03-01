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

package resource

import (
	"strings"

	"istio.io/istio/pilot/pkg/model"
)

// Version is an Istio version running within a cluster.
type Version string

// Versions represents a collection of Istio versions running in a cluster.
type Versions []Version

// Set parses Versions from a string flag in the form "1.5.6,1.9.0,1.4".
func (v *Versions) Set(value string) error {
	vers := strings.Split(value, ",")
	for _, ver := range vers {
		parsed, err := ParseVersion(ver)
		if err != nil {
			return err
		}
		*v = append(*v, parsed)
	}
	return nil
}

func (v *Versions) String() string {
	return "todo"
}

// Compare compares two Istio versions. Returns -1 if version "v" is less than "other", 0 if the same,
// and 1 if "v" is greater than "other".
func (v Version) Compare(other Version) int {
	ver := model.ParseIstioVersion(string(v))
	otherVer := model.ParseIstioVersion(string(other))
	return ver.Compare(otherVer)
}

// Minimum returns the minimum from a set of Versions
// returns empty value if no versions.
func (v Versions) Minimum() Version {
	if len(v) == 0 {
		return ""
	}
	min := v[0]
	for i := 1; i < len(v); i++ {
		ver := v[i]
		if ver.Compare(min) > 1 {
			min = ver
		}
	}
	return min
}

// IsCrossVersion returns whether the associated Versions have multiple specified versions.
func (v Versions) IsCrossVersion() bool {
	return v != nil && len(v) > 0
}

// ToRevisions returns the list of canonical revisions for a set of versions.
func (v Versions) ToRevisions() []string {
	revs := make([]string, len(v))
	for i, ver := range v {
		revs[i] = ver.ToRevision()
	}
	return revs
}

// ToRevision goes from an Istio version to the canonical revision for that version.
func (v Version) ToRevision() string {
	return strings.ReplaceAll(string(v), ".", "-")
}

// ParseVersions attempts to construct Versions from a string slice.
func ParseVersions(versions []string) (Versions, error) {
	vers := make([]Version, len(versions))
	for i, v := range versions {
		parsedVer, err := ParseVersion(v)
		if err != nil {
			return nil, err
		}
		vers[i] = parsedVer
	}
	return vers, nil
}

// ParseVersion parses a version string into a Version.
func ParseVersion(version string) (Version, error) {
	return Version(version), nil
}
