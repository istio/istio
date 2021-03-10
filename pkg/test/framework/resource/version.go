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

// RevVerMap maps installed revisions to their Istio versions.
type RevVerMap map[string]IstioVersion

// Set parses IstioVersions from a string flag in the form "a=1.5.6,b=1.9.0,c=1.4".
func (rv *RevVerMap) Set(value string) error {
	m := make(map[string]IstioVersion)
	rvPairs := strings.Split(value, ",")
	for _, rvPair := range rvPairs {
		s := strings.Split(rvPair, "=")
		rev, ver := s[0], s[1]
		parsedVer, err := ParseIstioVersion(ver)
		if err != nil {
			return err
		}
		m[rev] = parsedVer
	}
	*rv = m
	return nil
}

func (rv *RevVerMap) String() string {
	if rv == nil {
		return ""
	}
	var vers []string
	for _, ver := range *rv {
		vers = append(vers, string(ver))
	}
	return strings.Join(vers, ",")
}

// Versions returns an ordered list of Istio versions from the given RevVerMap.
func (rv *RevVerMap) Versions() IstioVersions {
	if rv == nil {
		return nil
	}
	var vers []IstioVersion
	for _, v := range *rv {
		vers = append(vers, v)
	}
	return vers
}

// Minimum returns the minimum version from the revision-version mapping.
func (rv *RevVerMap) Minimum() IstioVersion {
	return rv.Versions().Minimum()
}

// IsMultiVersion returns whether the associated IstioVersions have multiple specified versions.
func (rv *RevVerMap) IsMultiVersion() bool {
	return rv != nil && len(*rv) > 0
}

// TemplateMap creates a map of revisions and versions suitable for templating.
func (rv *RevVerMap) TemplateMap() map[string]string {
	if rv == nil {
		return nil
	}
	templateMap := make(map[string]string)
	if len(*rv) == 0 {
		// if there are no entries, generate a dummy value so that we don't render
		// deployment template with empty loop
		templateMap[""] = ""
		return templateMap
	}
	for r, v := range *rv {
		templateMap[r] = string(v)
	}
	return templateMap
}

// IstioVersion is an Istio version running within a cluster.
type IstioVersion string

// IstioVersions represents a collection of Istio versions running in a cluster.
type IstioVersions []IstioVersion

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

// ParseIstioVersion parses a version string into a IstioVersion.
func ParseIstioVersion(version string) (IstioVersion, error) {
	return IstioVersion(version), nil
}
