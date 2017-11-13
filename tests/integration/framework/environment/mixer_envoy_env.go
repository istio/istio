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
	"io/ioutil"
	"log"
	"path/filepath"

	"istio.io/istio/tests/integration/framework"
	"istio.io/istio/tests/integration/framework/component/fortio_server"
	"istio.io/istio/tests/integration/framework/component/mixer"
	"istio.io/istio/tests/integration/framework/component/proxy"
)

// MixerEnvoyEnv is a test environment with envoy, mixer and echo server
type MixerEnvoyEnv struct {
	framework.TestEnv
	EnvID string
}

// NewMixerEnvoyEnv create a MixerEnvoyEnv with a env ID
func NewMixerEnvoyEnv(id string) *MixerEnvoyEnv {
	return &MixerEnvoyEnv{
		EnvID: id,
	}
}

// GetName return environment ID
func (mixerEnvoyEnv *MixerEnvoyEnv) GetName() string {
	return mixerEnvoyEnv.EnvID
}

// Bringup doing setup for MixerEnvoyEnv
func (mixerEnvoyEnv *MixerEnvoyEnv) Bringup() (err error) {
	log.Printf("Bringing up %s", mixerEnvoyEnv.EnvID)
	return
}

// GetComponents returns a list of components, including mixer, proxy and fortio server
func (mixerEnvoyEnv *MixerEnvoyEnv) GetComponents() []framework.Component {
	logDir, err := ioutil.TempDir("", mixerEnvoyEnv.GetName())
	if err != nil {
		log.Printf("Failed to get a temp dir: %v", err)
		return nil
	}
	configDir := filepath.Join(logDir, "config")

	comps := []framework.Component{}
	comps = append(comps, fortioServer.NewLocalComponent("my_fortio_server", logDir))
	comps = append(comps, mixer.NewLocalComponent("my_local_mixer", logDir, configDir))
	comps = append(comps, proxy.NewLocalComponent("my_local_envory", logDir))

	return comps
}

// Cleanup clean everything created by MixerEnvoyEnv, not component level
func (mixerEnvoyEnv *MixerEnvoyEnv) Cleanup() (err error) {
	log.Printf("Cleaning up %s", mixerEnvoyEnv.EnvID)
	return
}
