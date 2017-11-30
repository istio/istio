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

package environment

import (
	"path/filepath"

	fortioServer "istio.io/istio/tests/integration/component/fortio_server"
	"istio.io/istio/tests/integration/component/mixer"
	"istio.io/istio/tests/integration/component/proxy"
	"istio.io/istio/tests/integration/framework"
)

// MixerEnvoyEnv is a test environment with envoy, mixer and echo server
type MixerEnvoyEnv struct {
	framework.TestEnv
	EnvID  string
	tmpDir string
}

// NewMixerEnvoyEnv create a MixerEnvoyEnv with a env ID
func NewMixerEnvoyEnv(id string) *MixerEnvoyEnv {
	return &MixerEnvoyEnv{
		EnvID: id,
	}
}

// GetComponents returns a list of components, including mixer, proxy and fortio server
func (mixerEnvoyEnv *MixerEnvoyEnv) GetComponents() []framework.Component {
	configDir := filepath.Join(mixerEnvoyEnv.tmpDir, "mixer_config")

	// Define what components this environment has
	comps := []framework.Component{}
	comps = append(comps, fortioServer.NewLocalComponent("my_fortio_server", mixerEnvoyEnv.tmpDir))
	comps = append(comps, mixer.NewLocalComponent("my_local_mixer", mixerEnvoyEnv.tmpDir, configDir))
	comps = append(comps, proxy.NewLocalComponent("my_local_envoy", mixerEnvoyEnv.tmpDir))

	return comps
}
