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

package image

import (
	"flag"
	"fmt"

	"istio.io/istio/pkg/test/env"
)

var (
	// Settings we will collect from the command-line.
	settingsFromCommandLine = &Settings{
		Hub:        env.HUB.ValueOrDefault("gcr.io/istio-testing"),
		Tag:        env.TAG.ValueOrDefault("latest"),
		PullPolicy: env.PULL_POLICY.Value(),
		BitnamiHub: env.BITNAMIHUB.ValueOrDefault("docker.io/bitnami"),
	}
)

// SettingsFromCommandLine returns Settings obtained from command-line flags. flag.Parse must be called before calling this function.
func SettingsFromCommandLine() (*Settings, error) {
	if !flag.Parsed() {
		panic("flag.Parse must be called before this function")
	}

	s := settingsFromCommandLine.clone()

	if s.PullPolicy == "" {
		// Default to pull-always
		s.PullPolicy = "Always"
	}

	if s.Hub == "" || s.Tag == "" {
		return nil, fmt.Errorf("values for Hub & Tag are not detected. Please supply them through command-line or via environment")
	}

	return s, nil
}

func init() {
	flag.StringVar(&settingsFromCommandLine.Hub, "istio.test.hub", settingsFromCommandLine.Hub,
		"Container registry hub to use")
	flag.StringVar(&settingsFromCommandLine.Tag, "istio.test.tag", settingsFromCommandLine.Tag,
		"Common Container tag to use when deploying container images")
	flag.StringVar(&settingsFromCommandLine.PullPolicy, "istio.test.pullpolicy", settingsFromCommandLine.PullPolicy,
		"Common image pull policy to use when deploying container images")
	flag.StringVar(&settingsFromCommandLine.BitnamiHub, "istio.test.bitnamihub", settingsFromCommandLine.BitnamiHub,
		"Container registry to use to download binami images for the redis tests")
}
