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

import "fmt"

type MajorVersion struct {
	Major uint32
}

type MinorVersion struct {
	MajorVersion
	Minor uint32
}

type PatchVersion struct {
	MinorVersion
	Patch uint32
}

type Version struct {
	PatchVersion
	Prefix string
	Suffix string
}

func NewMajorVersion(major uint32) MajorVersion {
	return MajorVersion{
		Major: major,
	}
}

func NewMinorVersion(major, minor uint32) MinorVersion {
	return MinorVersion{
		MajorVersion: NewMajorVersion(major),
		Minor:        minor,
	}
}

func NewPatchVersion(major, minor, patch uint32) PatchVersion {
	return PatchVersion{
		MinorVersion: NewMinorVersion(major, minor),
		Patch:        patch,
	}
}

func NewVersion(prefix string, major, minor, patch uint32, suffix string) Version {
	return Version{
		PatchVersion: NewPatchVersion(major, minor, patch),
		Prefix:       prefix,
		Suffix:       suffix,
	}
}

func (v MajorVersion) String() string {
	return fmt.Sprintf("%d", v.Major)
}

func (v MinorVersion) String() string {
	return fmt.Sprintf("%s.%d", v.MajorVersion, v.Minor)
}

func (v PatchVersion) String() string {
	return fmt.Sprintf("%s.%d", v.MinorVersion, v.Minor)
}

func (v Version) String() string {
	return fmt.Sprintf("%s%s%s", v.Prefix, v.PatchVersion, v.Suffix)
}
