// Copyright 2017-2018 Istio Authors
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

// Package version provides build time version information.
package version

import (
	"fmt"
	"runtime"
)

// The following fields are populated at build time using -ldflags -X.
// Note that DATE is omitted for reproducible builds
var (
	buildAppVersion  string
	buildGitRevision string
	buildUser        string
	buildHost        string
	buildDockerHub   string
)

// BuildInfo describes version information about the binary build.
type BuildInfo struct {
	Version       string `json:"version"`
	GitRevision   string `json:"revision"`
	User          string `json:"user"`
	Host          string `json:"host"`
	GolangVersion string `json:"golang_version"`
	DockerHub     string `json:"hub"`
}

var (
	// Info exports the build version information.
	Info BuildInfo
)

func init() {
	Info.Version = buildAppVersion
	Info.GitRevision = buildGitRevision
	Info.User = buildUser
	Info.Host = buildHost
	Info.GolangVersion = runtime.Version()
	Info.DockerHub = buildDockerHub
}

// Line combines version information into a single line
func Line() string {
	return fmt.Sprintf("%v@%v-%v-%v-%v",
		Info.User,
		Info.Host,
		Info.DockerHub,
		Info.Version,
		Info.GitRevision)
}

// Version returns a multi-line version information
func Version() string {
	return fmt.Sprintf(`Version: %v
GitRevision: %v
User: %v@%v
Hub: %v
GolangVersion: %v
`,
		Info.Version,
		Info.GitRevision,
		Info.User, Info.Host,
		Info.DockerHub,
		Info.GolangVersion)
}
