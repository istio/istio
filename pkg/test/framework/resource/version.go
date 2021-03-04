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

// IstioVersion is an Istio version running within a cluster.
type IstioVersion string

// IstioVersions represents a collection of Istio versions running in a cluster.
type IstioVersions []IstioVersion

// Set parses IstioVersions from a string flag in the form "1.5.6,1.9.0,1.4".
func (v *IstioVersions) Set(value string) error {
	vers := strings.Split(value, ",")
	for _, ver := range vers {
		parsed, err := ParseIstioVersion(ver)
		if err != nil {
			return err
		}
		*v = append(*v, parsed)
	}
	return nil
}

func (v *IstioVersions) String() string {
	if v == nil {
		return ""
	}
	var vers []string
	for _, ver := range *v {
		vers = append(vers, string(ver))
	}
	return strings.Join(vers, ",")
}

// Compare compares two Istio versions. Returns -1 if version "v" is less than "other", 0 if the same,
// and 1 if "v" is greater than "other".
func (v IstioVersion) Compare(other IstioVersion) int {
	ver := model.ParseIstioVersion(string(v))
	otherVer := model.ParseIstioVersion(string(other))
	return ver.Compare(otherVer)
}

// Minimum returns the minimum from a set of IstioVersions
// returns empty value if no versions.
func (v IstioVersions) Minimum() IstioVersion {
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

// IsMultiVersion returns whether the associated IstioVersions have multiple specified versions.
func (v IstioVersions) IsMultiVersion() bool {
	return v != nil && len(v) > 0
}

// RevisionLabels returns the list of canonical revisions for a set of versions.
func (v IstioVersions) RevisionLabels() []string {
	revs := make([]string, len(v))
	for i, ver := range v {
		revs[i] = ver.RevisionLabel()
	}
	return revs
}

// RevisionLabel goes from an Istio version to the canonical revision for that version.
func (v IstioVersion) RevisionLabel() string {
	return strings.ReplaceAll(string(v), ".", "-")
}

// ParseIstioVersion parses a version string into a IstioVersion.
func ParseIstioVersion(version string) (IstioVersion, error) {
	return IstioVersion(version), nil
}
