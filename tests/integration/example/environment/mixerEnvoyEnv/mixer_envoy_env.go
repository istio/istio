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

package mixerEnvoyEnv

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	fortioServer "istio.io/istio/tests/integration/component/fortio_server"
	"istio.io/istio/tests/integration/component/mixer"
	"istio.io/istio/tests/integration/component/proxy"
	"istio.io/istio/tests/integration/framework"
)

// MixerEnvoyEnv is a test environment with envoy, mixer and echo server
type MixerEnvoyEnv struct {
	framework.TestEnv
	EnvID     string
	TmpDir    string
	configDir string
}

// NewMixerEnvoyEnv create a MixerEnvoyEnv with a env ID
func NewMixerEnvoyEnv(id string) *MixerEnvoyEnv {
	return &MixerEnvoyEnv{
		EnvID: id,
	}
}

// GetName implement the function in TestEnv return environment ID
func (mixerEnvoyEnv *MixerEnvoyEnv) GetName() string {
	return mixerEnvoyEnv.EnvID
}

// Bringup implement the function in TestEnv
// Create local temp dir. Can be manually overrided if necessary.
func (mixerEnvoyEnv *MixerEnvoyEnv) Bringup() (err error) {
	log.Printf("Bringing up %s", mixerEnvoyEnv.EnvID)
	mixerEnvoyEnv.TmpDir, err = ioutil.TempDir("", mixerEnvoyEnv.GetName())
	return
}

// GetComponents returns a list of components, including mixer, proxy and fortio server
func (mixerEnvoyEnv *MixerEnvoyEnv) GetComponents() []framework.Component {
	mixerEnvoyEnv.configDir = filepath.Join(mixerEnvoyEnv.TmpDir, "mixer_config")

	// Define what components this environment has
	comps := []framework.Component{}
	comps = append(comps, fortioServer.NewLocalComponent("my_fortio_server", mixerEnvoyEnv.TmpDir))
	comps = append(comps, mixer.NewLocalComponent("my_local_mixer", mixerEnvoyEnv.TmpDir, mixerEnvoyEnv.configDir))
	comps = append(comps, proxy.NewLocalComponent("my_local_envoy", mixerEnvoyEnv.TmpDir))

	return comps
}

// Cleanup implement the function in TestEnv
// Remove the local temp dir. Can be manually overrided if necessary.
func (mixerEnvoyEnv *MixerEnvoyEnv) Cleanup() (err error) {
	log.Printf("Cleaning up %s", mixerEnvoyEnv.EnvID)
	os.RemoveAll(mixerEnvoyEnv.TmpDir)
	os.RemoveAll(mixerEnvoyEnv.configDir)
	return
}
