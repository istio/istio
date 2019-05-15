// Copyright 2018 Istio Authors
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

// Package version provides build version information.
package version

import (
	"fmt"
	"runtime"
	"strings"
)

// The following fields are populated at build time using -ldflags -X.
// Note that DATE is omitted for reproducible builds
var (
	buildVersion     = "unknown"
	buildGitRevision = "unknown"
	buildUser        = "unknown"
	buildHost        = "unknown"
	buildDockerHub   = "unknown"
	buildStatus      = "unknown"
	buildTag         = "unknown"
)

// BuildInfo describes version information about the binary build.
type BuildInfo struct {
	Version       string `json:"version"`
	GitRevision   string `json:"revision"`
	User          string `json:"user"`
	Host          string `json:"host"`
	GolangVersion string `json:"golang_version"`
	DockerHub     string `json:"hub"`
	BuildStatus   string `json:"status"`
	GitTag        string `json:"tag"`
}

// ServerInfo contains the version for a single control plane component
type ServerInfo struct {
	Component string
	Info      BuildInfo
}

// MeshInfo contains the versions for all Istio control plane components
type MeshInfo []ServerInfo

// NewBuildInfoFromOldString creates a BuildInfo struct based on the output
// of previous Istio components '-- version' output
func NewBuildInfoFromOldString(oldOutput string) (BuildInfo, error) {
	res := BuildInfo{}

	lines := strings.Split(oldOutput, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.SplitN(line, ":", 2)
		if fields != nil {
			if len(fields) != 2 {
				return BuildInfo{}, fmt.Errorf("invalid BuildInfo input, field '%s' is not valid", fields[0])
			}
			value := strings.TrimSpace(fields[1])
			switch fields[0] {
			case "Version":
				res.Version = value
			case "GitRevision":
				res.GitRevision = value
			case "User":
				res.User = value
			case "Hub":
				res.DockerHub = value
			case "GolangVersion":
				res.GolangVersion = value
			case "BuildStatus":
				res.BuildStatus = value
			case "GitTag":
				res.GitTag = value
			default:
				return BuildInfo{}, fmt.Errorf("invalid BuildInfo input, field '%s' is not valid", fields[0])
			}
		}
	}

	return res, nil
}

var (
	// Info exports the build version information.
	Info BuildInfo
)

// String produces a single-line version info
//
// This looks like:
//
// ```
// user@host-<docker hub>-<version>-<git revision>-<build status>
// ```
func (b BuildInfo) String() string {
	return fmt.Sprintf("%v@%v-%v-%v-%v-%v",
		b.User,
		b.Host,
		b.DockerHub,
		b.Version,
		b.GitRevision,
		b.BuildStatus)
}

// LongForm returns a dump of the Info struct
// This looks like:
//
func (b BuildInfo) LongForm() string {
	return fmt.Sprintf("%#v", b)
}

func init() {
	Info = BuildInfo{
		Version:       buildVersion,
		GitRevision:   buildGitRevision,
		User:          buildUser,
		Host:          buildHost,
		GolangVersion: runtime.Version(),
		DockerHub:     buildDockerHub,
		BuildStatus:   buildStatus,
		GitTag:        buildTag,
	}
}
