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

package configdump

import (
	"fmt"

	any "google.golang.org/protobuf/types/known/anypb"
)

type configTypeURL string

// See https://www.envoyproxy.io/docs/envoy/latest/api-v3/admin/v3/config_dump.proto
const (
	bootstrap configTypeURL = "type.googleapis.com/envoy.admin.v3.BootstrapConfigDump"
	listeners configTypeURL = "type.googleapis.com/envoy.admin.v3.ListenersConfigDump"
	endpoints configTypeURL = "type.googleapis.com/envoy.admin.v3.EndpointsConfigDump"
	clusters  configTypeURL = "type.googleapis.com/envoy.admin.v3.ClustersConfigDump"
	routes    configTypeURL = "type.googleapis.com/envoy.admin.v3.RoutesConfigDump"
	secrets   configTypeURL = "type.googleapis.com/envoy.admin.v3.SecretsConfigDump"
)

// getSection takes a TypeURL and returns the types.Any from the config dump corresponding to that URL
func (w *Wrapper) getSection(sectionTypeURL configTypeURL) (*any.Any, error) {
	var dumpAny *any.Any
	for _, conf := range w.Configs {
		if conf.TypeUrl == string(sectionTypeURL) {
			dumpAny = conf
		}
	}
	if dumpAny == nil {
		return nil, fmt.Errorf("config dump has no configuration type %s", sectionTypeURL)
	}

	return dumpAny, nil
}
