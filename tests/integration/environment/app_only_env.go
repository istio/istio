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
	"log"

	"istio.io/istio/tests/integration/framework"
	"istio.io/istio/tests/integration/framework/component/fortio_server"
)

// AppOnlyEnv is a test environment with only fortio echo server
type AppOnlyEnv struct {
	framework.TestEnv
	EnvID    string
	EndPoint string
}

// NewAppOnlyEnv create an AppOnlyEnv with a env ID
func NewAppOnlyEnv(id string) *AppOnlyEnv {
	return &AppOnlyEnv{
		EnvID: id,
	}
}

// GetName return environment ID
func (appOnlyEnv *AppOnlyEnv) GetName() string {
	return appOnlyEnv.EnvID
}

// Bringup doing setup for AppOnlyEnv
func (appOnlyEnv *AppOnlyEnv) Bringup() (err error) {
	log.Printf("Bringing up %s", appOnlyEnv.EnvID)
	return
}

// GetComponents returns a list with a fortio server component
func (appOnlyEnv *AppOnlyEnv) GetComponents() []framework.Component {
	comps := []framework.Component{}
	comps = append(comps, fortioServer.NewLocalComponent("my_fortio_server", "/tmp"))

	return comps
}

// Cleanup clean everything created by AppOnlyEnv, not component level
func (appOnlyEnv *AppOnlyEnv) Cleanup() (err error) {
	log.Printf("Cleaning up %s", appOnlyEnv.EnvID)
	return
}
