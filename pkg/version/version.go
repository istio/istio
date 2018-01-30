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

// LongForm returns a multi-line version information
//
// This looks like:
//
// ```
// Version: <version>
// GitRevision: <git revision>
// User: user@host
// Hub: <docker hub>
// GolangVersion: go1.9.2
// BuildStatus: <build status>
// ```
func (b BuildInfo) LongForm() string {
	return fmt.Sprintf(`Version: %v
GitRevision: %v
User: %v@%v
Hub: %v
GolangVersion: %v
BuildStatus: %v
`,
		b.Version,
		b.GitRevision,
		b.User,
		b.Host,
		b.DockerHub,
		b.GolangVersion,
		b.BuildStatus)
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
	}
}
