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
	"io/ioutil"
	"log"
	"os"

	fortioServer "istio.io/istio/tests/integration/component/fortio_server"
	"istio.io/istio/tests/integration/framework"
)

// AppOnlyEnv is a test environment with only fortio echo server
type AppOnlyEnv struct {
	framework.TestEnv
	EnvID  string
	TmpDir string
}

// NewAppOnlyEnv create an AppOnlyEnv with a env ID
func NewAppOnlyEnv(id string) *AppOnlyEnv {
	return &AppOnlyEnv{
		EnvID: id,
	}
}

// GetName implement the function in TestEnv return environment ID
func (appOnlyEnv *AppOnlyEnv) GetName() string {
	return appOnlyEnv.EnvID
}

// Bringup implement the function in TestEnv
// Create local temp dir. Can be manually overrided if necessary.
func (appOnlyEnv *AppOnlyEnv) Bringup() (err error) {
	log.Printf("Bringing up %s", appOnlyEnv.EnvID)
	appOnlyEnv.TmpDir, err = ioutil.TempDir("", appOnlyEnv.GetName())
	return
}

// GetComponents returns a list with a fortio server component
func (appOnlyEnv *AppOnlyEnv) GetComponents() []framework.Component {
	// Define what components this environment has
	comps := []framework.Component{}
	comps = append(comps, fortioServer.NewLocalComponent("my_fortio_server", appOnlyEnv.TmpDir))

	return comps
}

// Cleanup implement the function in TestEnv
// Remove the local temp dir. Can be manually overrided if necessary.
func (appOnlyEnv *AppOnlyEnv) Cleanup() (err error) {
	log.Printf("Cleaning up %s", appOnlyEnv.EnvID)
	_ = os.RemoveAll(appOnlyEnv.TmpDir)
	return
}
