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

			if showKubeInjectInfo {
				fmt.Printf("KubeInjectHub: %v\n", KubeInjectHub)
				fmt.Printf("KubeInjectTag: %v\n", KubeInjectTag)
			}
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

	VersionCmd.PersistentFlags().BoolVar(&showKubeInjectInfo, "kube-inject", false,
		"Show kube-inject docker hub and tag info")
	VersionCmd.PersistentFlags().MarkHidden("kube-inject") // nolint: errcheck, gas
}

// Line combines version information into a single line
func Line() string {
	return fmt.Sprintf("%v@%v-%v-%v",
		Info.User,
		Info.Host,
		Info.Version,
		Info.GitRevision)
}

// Temporary workaround to make parameterize docker hub and tag for
// kube-inject. Ideally this would be in cmd/istioctl/inject.go but
// bazel's linkstamp feature requires all symbols to be in the same
// golang package. This should go away once we switch over to
// server-side kube-inject or k8s initializer.
var (
	showKubeInjectInfo bool

	// KubeInjectHub is the linker specified docker hub for kube-inject.
	KubeInjectHub string
	// KubeInjectTag is the linker specified docker tag for kube-inject.
	KubeInjectTag string
)
