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
	fortioServer "istio.io/istio/tests/integration/component/fortio_server"
	"istio.io/istio/tests/integration/framework"
)

// AppOnlyEnv is a test environment with only fortio echo server
type AppOnlyEnv struct {
	framework.CommonEnv
}

// NewAppOnlyEnv create an AppOnlyEnv with a env ID
func NewAppOnlyEnv(id string) *AppOnlyEnv {
	return &AppOnlyEnv{
		CommonEnv: framework.CommonEnv{
			EnvID: id,
		},
	}
}

// GetComponents returns a list with a fortio server component
func (appOnlyEnv *AppOnlyEnv) GetComponents() []framework.Component {
	// Define what components this environment has
	comps := []framework.Component{}
	comps = append(comps, fortioServer.NewLocalComponent("my_fortio_server", appOnlyEnv.TmpDir))

	return comps
}
