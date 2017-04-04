// Copyright 2017 Istio Authors
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

	"github.com/spf13/cobra"
)

// The following fields are populated at buildtime with bazel's linkstamp
// feature. This is equivalent to using golang directly with -ldflags -X.
// Note that DATE is omitted as bazel aims for reproducible builds and
// seems to strip date information from the build process.
var (
	buildAppVersion  string
	buildGitRevision string
	buildGitBranch   string
	buildUser        string
	buildHost        string
)

// BuildInfo describes version information about the binary build.
type BuildInfo struct {
	Version       string `json:"version"`
	GitRevision   string `json:"revision"`
	GitBranch     string `json:"branch"`
	User          string `json:"user"`
	Host          string `json:"host"`
	GolangVersion string `json:"golang_version"`
}

var (
	// Info exports the build version information.
	Info BuildInfo

	// VersionCmd provides a command to query the version of Istio Manager
	VersionCmd = &cobra.Command{
		Use:   "version",
		Short: "Display version information and exit",
		Run: func(*cobra.Command, []string) {
			fmt.Printf("Version: %v\n", Info.Version)
			fmt.Printf("GitRevision: %v\n", Info.GitRevision)
			fmt.Printf("GitBranch: %v\n", Info.GitBranch)
			fmt.Printf("User: %v@%v\n", Info.User, Info.Host)
			fmt.Printf("GolangVersion: %v\n", Info.GolangVersion)
		},
	}
)

func init() {
	Info.Version = buildAppVersion
	Info.GitRevision = buildGitRevision
	Info.GitBranch = buildGitBranch
	Info.User = buildUser
	Info.Host = buildHost
	Info.GolangVersion = runtime.Version()
}

// Line combines version information into a single line
func Line() string {
	return fmt.Sprintf("%v@%v-%v-%v",
		Info.User,
		Info.Host,
		Info.Version,
		Info.GitRevision)
}
