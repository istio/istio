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

// Package version for fortio holds version information and build information.
package version // import "istio.io/fortio/version"
import (
	"fmt"
	"runtime"

	"istio.io/fortio/log"
)

const (
	major = 1
	minor = 0
	patch = 0

	debug = false // turn on to debug init()
)

var (
	// The following are set by Dockerfile during link time:
	tag       = "n/a"
	buildInfo = "unknown"
	// Number of lines in git status --porcelain; 0 means clean
	gitstatus = "0" // buildInfo default is unknown so no need to add -dirty
	// computed in init()
	version     = ""
	longVersion = ""
)

// Major returns the numerical major version number (first digit of version.Short()).
func Major() int {
	return major
}

// Minor returns the numerical minor version number (second digit of version.Short()).
func Minor() int {
	return minor
}

// Patch returns the numerical patch level (third digit of version.Short()).
func Patch() int {
	return patch
}

// Short returns the 3 digit short version string Major.Minor.Patch[-pre]
// version.Short() is the overall project version (used to version json
// output too). "-pre" is added when the version doesn't match exactly
// a git tag or the build isn't from a clean source tree. (only standard
// dockerfile based build of a clean, tagged source tree should print "X.Y.Z"
// as short version).
func Short() string {
	return version
}

// Long returns the full version and build information.
// Format is "X.Y.X[-pre] YYYY-MM-DD HH:MM SHA[-dirty]" date and time is
// the build date (UTC), sha is the git sha of the source tree.
func Long() string {
	return longVersion
}

// Carefully manually tested all the combinations in pair with Dockerfile
func init() {
	if debug {
		log.SetLogLevel(log.Debug)
	}
	version = fmt.Sprintf("%d.%d.%d", major, minor, patch)
	clean := (gitstatus == "0")
	// The docker build will pass the git tag to the build, if it is clean
	// from a tag it will look like v0.7.0
	if tag != "v"+version || !clean {
		log.Debugf("tag is %v, clean is %v marking as pre release", tag, clean)
		version += "-pre"
	}
	if !clean {
		buildInfo += "-dirty"
		log.Debugf("gitstatus is %q, marking buildinfo as dirty: %v", gitstatus, buildInfo)
	}
	longVersion = version + " " + buildInfo + " " + runtime.Version()
}
