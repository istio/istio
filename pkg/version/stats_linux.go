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

package version

import (
	"istio.io/istio/pkg/monitoring"
)

// Note that this code uses go.opencensus.io/stats,
// which depends on google.golang.org/grpc/internal/channelz,
// which is not available for OSX.  The build tag on line 1
// ensures this is only built on Linux.

var (
	gitTagKey       = monitoring.MustCreateLabel("tag")
	componentTagKey = monitoring.MustCreateLabel("component")
	istioBuildTag   = monitoring.NewGauge(
		"istio_build",
		"Istio component build info",
		monitoring.WithLabels(gitTagKey, componentTagKey))
)

// RecordComponentBuildTag sets the value for a metric that will be used to track component build tags for
// tracking rollouts, etc.
func (b BuildInfo) RecordComponentBuildTag(component string) {
	istioBuildTag.With(gitTagKey.Value(b.GitTag), componentTagKey.Value(component)).Increment()
}

func init() {
	monitoring.MustRegister(istioBuildTag)
}
