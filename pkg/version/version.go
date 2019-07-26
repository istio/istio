// Copyright 2019 Istio Authors
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

package version

import (
	"fmt"
	"strings"

	goversion "github.com/hashicorp/go-version"
)

// CompatibilityMapping is a mapping from an Istio operator version and the corresponding recommended and
// supported versions of Istio.
type CompatibilityMapping struct {
	OperatorVersion          *goversion.Version    `json:"operatorVersion,omitempty"`
	SupportedIstioVersions   goversion.Constraints `json:"supportedIstioVersions,omitempty"`
	RecommendedIstioVersions goversion.Constraints `json:"recommendedIstioVersions,omitempty"`
}

// NewVersionFromString creates a new Version from the provided SemVer formatted string and returns a pointer to it.
func NewVersionFromString(s string) (*Version, error) {
	ver, err := goversion.NewVersion(s)
	if err != nil {
		return nil, err
	}

	newVer := &Version{}
	vv := ver.Segments()
	if len(vv) > 0 {
		newVer.Major = uint32(vv[0])
	}
	if len(vv) > 1 {
		newVer.Minor = uint32(vv[1])
	}
	if len(vv) > 2 {
		newVer.Patch = uint32(vv[2])
	}

	sv := strings.Split(s, "-")
	if len(sv) > 0 {
		newVer.Suffix = strings.Join(sv[1:], "-")
	}

	return newVer, nil
}

// MarshalYAML implements the Marshaler interface.
func (v *CompatibilityMapping) MarshalYAML() (interface{}, error) {
	out := make(map[string]string)
	if v.OperatorVersion != nil {
		out["operatorVersion"] = v.OperatorVersion.String()
	}
	if v.SupportedIstioVersions != nil {
		out["supportedIstioVersions"] = v.SupportedIstioVersions.String()
	}
	if v.RecommendedIstioVersions != nil {
		out["recommendedIstioVersions"] = v.RecommendedIstioVersions.String()
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// UnmarshalYAML implements the Unmarshaler interface.
func (v *CompatibilityMapping) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type inStruct struct {
		OperatorVersion          string `yaml:"operatorVersion"`
		SupportedIstioVersions   string `yaml:"supportedIstioVersions"`
		RecommendedIstioVersions string `yaml:"recommendedIstioVersions"`
	}
	tmp := inStruct{}
	if err := unmarshal(&tmp); err != nil {
		return err
	}

	if tmp.OperatorVersion == "" {
		return fmt.Errorf("operatorVersion must be set")
	}
	if tmp.SupportedIstioVersions == "" {
		return fmt.Errorf("supportedIstioVersions must be set")
	}

	var err error
	v.OperatorVersion, err = goversion.NewVersion(tmp.OperatorVersion)
	if err != nil {
		return err
	}
	v.SupportedIstioVersions, err = goversion.NewConstraint(tmp.SupportedIstioVersions)
	if err != nil {
		return err
	}
	if tmp.RecommendedIstioVersions != "" {
		v.RecommendedIstioVersions, err = goversion.NewConstraint(tmp.RecommendedIstioVersions)
		if err != nil {
			return err
		}
	}
	return nil
}

// MajorVersion represents a major version.
type MajorVersion struct {
	Major uint32
}

// MinorVersion represents a minor version.
type MinorVersion struct {
	MajorVersion
	Minor uint32
}

// PatchVersion represents a patch version.
type PatchVersion struct {
	MinorVersion
	Patch uint32
}

// Version represents a version with an optional suffix.
type Version struct {
	PatchVersion
	Suffix string
}

// NewMajorVersion creates an initialized MajorVersion struct.
func NewMajorVersion(major uint32) MajorVersion {
	return MajorVersion{
		Major: major,
	}
}

// NewMinorVersion creates an initialized MinorVersion struct.
func NewMinorVersion(major, minor uint32) MinorVersion {
	return MinorVersion{
		MajorVersion: NewMajorVersion(major),
		Minor:        minor,
	}
}

// NewPatchVersion creates an initialized PatchVersion struct.
func NewPatchVersion(major, minor, patch uint32) PatchVersion {
	return PatchVersion{
		MinorVersion: NewMinorVersion(major, minor),
		Patch:        patch,
	}
}

// NewVersion creates an initialized Version struct.
func NewVersion(major, minor, patch uint32, suffix string) Version {
	return Version{
		PatchVersion: NewPatchVersion(major, minor, patch),
		Suffix:       suffix,
	}
}

// String implements the Stringer interface.
func (v MajorVersion) String() string {
	return fmt.Sprintf("%d", v.Major)
}

// String implements the Stringer interface.
func (v MinorVersion) String() string {
	return fmt.Sprintf("%s.%d", v.MajorVersion, v.Minor)
}

// String implements the Stringer interface.
func (v PatchVersion) String() string {
	return fmt.Sprintf("%s.%d", v.MinorVersion, v.Patch)
}

// String implements the Stringer interface.
func (v *Version) String() string {
	return fmt.Sprintf("%s-%s", v.PatchVersion, v.Suffix)
}

// UnmarshalYAML implements the Unmarshaler interface.
func (v *Version) UnmarshalYAML(unmarshal func(interface{}) error) error {
	s := ""
	if err := unmarshal(&s); err != nil {
		return err
	}
	out, err := NewVersionFromString(s)
	if err != nil {
		return err
	}
	*v = *out
	return nil
}
