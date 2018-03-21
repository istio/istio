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

package appOnlyEnv

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	fortioServer "istio.io/istio/tests/integration/component/fortio_server"
	"istio.io/istio/tests/integration/framework"
)

// AppOnlyEnv is a test environment with only fortio echo server
type AppOnlyEnv struct {
	framework.TestEnv
	envID  string
	tmpDir string
	comps  []framework.Component
}

// NewAppOnlyEnv create an AppOnlyEnv with a env ID
func NewAppOnlyEnv(id string) *AppOnlyEnv {
	return &AppOnlyEnv{
		envID: id,
	}
}

// GetName implement the function in TestEnv return environment ID
func (appOnlyEnv *AppOnlyEnv) GetName() string {
	return appOnlyEnv.envID
}

// Bringup implement the function in TestEnv
// Create local temp dir. Can be manually overrided if necessary.
func (appOnlyEnv *AppOnlyEnv) Bringup() (err error) {
	log.Printf("Bringing up %s", appOnlyEnv.envID)
	appOnlyEnv.tmpDir, err = ioutil.TempDir("", appOnlyEnv.GetName())
	log.Println(appOnlyEnv.tmpDir)
	return
}

// GetComponents returns a list with a fortio server component
func (appOnlyEnv *AppOnlyEnv) GetComponents() []framework.Component {
	// If it's the first time GetComponents being called, i.e. comps hasn't been defined yet
	if appOnlyEnv.comps == nil {
		// Define what components this environment has
		appOnlyEnv.comps = []framework.Component{}
		appOnlyEnv.comps = append(appOnlyEnv.comps, fortioServer.NewLocalComponent("my_fortio_server",
			fortioServer.LocalCompConfig{
				LogFile: fmt.Sprintf("%s/%s.log", appOnlyEnv.tmpDir, "my_local_fortio"),
			}))
	}

	return appOnlyEnv.comps
}

// Cleanup implement the function in TestEnv
// Remove the local temp dir. Can be manually overrided if necessary.
func (appOnlyEnv *AppOnlyEnv) Cleanup() (err error) {
	log.Printf("Cleaning up %s", appOnlyEnv.envID)
	_ = os.RemoveAll(appOnlyEnv.tmpDir)
	return
}
