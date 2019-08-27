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

package mixerenvoyenv

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	fortioServer "istio.io/istio/tests/integration_old/component/fortio_server"
	"istio.io/istio/tests/integration_old/component/mixer"
	"istio.io/istio/tests/integration_old/component/proxy"
	"istio.io/istio/tests/integration_old/framework"
)

// MixerEnvoyEnv is a test environment with envoy, mixer and echo server
type MixerEnvoyEnv struct {
	framework.TestEnv
	envID     string
	tmpDir    string
	configDir string
	comps     []framework.Component
}

// NewMixerEnvoyEnv create a MixerEnvoyEnv with a env ID
func NewMixerEnvoyEnv(id string) *MixerEnvoyEnv {
	return &MixerEnvoyEnv{
		envID: id,
	}
}

// GetName implement the function in TestEnv return environment ID
func (mixerEnvoyEnv *MixerEnvoyEnv) GetName() string {
	return mixerEnvoyEnv.envID
}

// Bringup implement the function in TestEnv
// Create local temp dir. Can be manually overrided if necessary.
func (mixerEnvoyEnv *MixerEnvoyEnv) Bringup() (err error) {
	log.Printf("Bringing up %s", mixerEnvoyEnv.envID)
	mixerEnvoyEnv.tmpDir, err = ioutil.TempDir("", mixerEnvoyEnv.GetName())
	log.Println(mixerEnvoyEnv.tmpDir)
	return
}

// GetComponents returns a list of components, including mixer, proxy and fortio server
func (mixerEnvoyEnv *MixerEnvoyEnv) GetComponents() []framework.Component {
	// If it's the first time GetComponents being called, i.e. comps hasn't been defined yet
	if mixerEnvoyEnv.comps == nil {
		// Define what components this environment has
		mixerEnvoyEnv.configDir = filepath.Join(mixerEnvoyEnv.tmpDir, "mixer_config")
		mixerEnvoyEnv.comps = []framework.Component{}
		mixerEnvoyEnv.comps = append(mixerEnvoyEnv.comps,
			fortioServer.NewLocalComponent("my_fortio_server",
				fortioServer.LocalCompConfig{
					LogFile: fmt.Sprintf("%s/%s.log", mixerEnvoyEnv.tmpDir, "my_local_fortio"),
				}),
			proxy.NewLocalComponent("my_local_envoy",
				proxy.LocalCompConfig{
					LogFile: fmt.Sprintf("%s/%s.log", mixerEnvoyEnv.tmpDir, "my_local_proxy"),
				}),
			mixer.NewLocalComponent("my_local_mixer",
				mixer.LocalCompConfig{
					ConfigFileDir: mixerEnvoyEnv.configDir,
					LogFile:       fmt.Sprintf("%s/%s.log", mixerEnvoyEnv.tmpDir, "my_local_mixer"),
				}))
	}

	return mixerEnvoyEnv.comps
}

// Cleanup implement the function in TestEnv
// Remove the local temp dir. Can be manually overrided if necessary.
func (mixerEnvoyEnv *MixerEnvoyEnv) Cleanup() (err error) {
	log.Printf("Cleaning up %s", mixerEnvoyEnv.envID)
	_ = os.RemoveAll(mixerEnvoyEnv.tmpDir)
	_ = os.RemoveAll(mixerEnvoyEnv.configDir)
	return
}
