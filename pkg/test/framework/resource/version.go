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
	"fmt"
	"sort"
	"strconv"
	"strings"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/framework/config"
)

var _ config.Value = &RevVerMap{}

// RevVerMap maps installed revisions to their Istio versions.
type RevVerMap map[string]IstioVersion

func (rv *RevVerMap) SetConfig(mi any) error {
	m, ok := mi.(config.Map)
	if !ok {
		return fmt.Errorf("revisions map: expected map but got slice")
	}
	out := make(RevVerMap)
	for k := range m {
		version := m.String(k)
		v, err := NewIstioVersion(version)
		if err != nil {
			return fmt.Errorf("could not parse %s as version: %w",
				version, err)
		}
		out[k] = v
	}
	*rv = out
	return nil
}

// Set parses IstioVersions from a string flag in the form "a=1.5.6,b,c=1.4".
// If no version is specified for a revision assume latest, represented as ""
func (rv *RevVerMap) Set(value string) error {
	m := make(map[string]IstioVersion)
	rvPairs := strings.Split(value, ",")
	for _, rv := range rvPairs {
		s := strings.Split(rv, "=")
		rev := s[0]
		if len(s) == 1 {
			m[rev] = ""
		} else if len(s) == 2 {
			ver := s[1]
			v, err := NewIstioVersion(ver)
			if err != nil {
				return fmt.Errorf("could not parse %s as version: %w",
					ver, err)
			}
			m[rev] = v
		} else {
			return fmt.Errorf("invalid revision<->version pairing specified: %q", rv)
		}
	}
	*rv = m
	return nil
}

func (rv *RevVerMap) String() string {
	if rv == nil {
		return ""
	}
	var rvPairs []string
	for rev, ver := range *rv {
		if ver == "" {
			ver = "latest"
		}
		rvPairs = append(rvPairs,
			fmt.Sprintf("%s=%s", rev, ver))
	}
	return strings.Join(rvPairs, ",")
}

// Versions returns the Istio versions present in the given RevVerMap.
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

// Default returns the revision with the newest `IstioVersion`, or in the case of a tie, the first
// alphabetically.
func (rv *RevVerMap) Default() string {
	if rv == nil || len(*rv) == 0 {
		return ""
	}
	max := rv.Maximum()
	var candidates []string
	for rev, ver := range *rv {
		if ver.Compare(max) == 0 {
			candidates = append(candidates, rev)
		}
	}
	if len(candidates) == 0 {
		panic("could not find revision with max IstioVersion")
	}
	sort.Strings(candidates)
	if candidates[0] == "default" {
		return ""
	}
	return candidates[0]
}

// Minimum returns the minimum version from the revision-version mapping.
func (rv *RevVerMap) Minimum() IstioVersion {
	return rv.Versions().Minimum()
}

// Maximum returns the maximum version from the revision-version mapping.
func (rv *RevVerMap) Maximum() IstioVersion {
	return rv.Versions().Maximum()
}

// AtLeast returns true if the minimum Istio version under test is at least the given version.
func (rv *RevVerMap) AtLeast(v IstioVersion) bool {
	return rv.Versions().Minimum().Compare(v) >= 0
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

// NewIstioVersion creates an IstioVersion with validation.
func NewIstioVersion(s string) (IstioVersion, error) {
	// empty version string sentinel value for latest
	if s == "" {
		return "", nil
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return "", fmt.Errorf("cannot parse version from %s", s)
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return "", fmt.Errorf("cannot use %s as version part", part)
		}
	}
	return IstioVersion(s), nil
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
		if ver.Compare(min) < 0 {
			min = ver
		}
	}
	return min
}

// Maximum returns the maximum from a set of IstioVersions
// returns empty value if no versions.
func (v IstioVersions) Maximum() IstioVersion {
	if len(v) == 0 {
		return ""
	}
	max := v[0]
	for i := 1; i < len(v); i++ {
		ver := v[i]
		if ver.Compare(max) > 0 {
			max = ver
		}
	}
	return max
}
