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

// Package version provides utilities for reporting build information (including
// version, build id, and status) for broker.
package version

import (
	"fmt"

	_ "github.com/golang/glog" // import glog flags
)

var (
	buildVersion = "unknown"
	buildID      = "unknown"
	buildStatus  = "unknown"
)

// BuildInfo holds build-related information for Mixer.
type BuildInfo struct {
	// Version is the tagged build version taken from SCM information.
	// Typically, this is from a `git describe` command.
	Version string

	// ID is a generated build ID. For broker, by convention, we use:
	// YYYY-MM-DD-SHA, where SHA is the short SHA extracted from SCM
	// (typically, `git rev-parse --short HEAD`).
	ID string

	// Status is used to indicate if the build was done against a clean
	// checkout at the ID specified, or if there were local file
	// modifications. Typically, this should either be `Clean` or `Modified`.
	Status string
}

func (b BuildInfo) String() string {
	return fmt.Sprintf("version: %s (build: %s, status: %s)", b.Version, b.ID, b.Status)
}

// Info is used to export the build info values set at runtime via `-ldflags`.
var Info BuildInfo

func init() {
	Info.Version = buildVersion
	Info.ID = buildID
	Info.Status = buildStatus
}
