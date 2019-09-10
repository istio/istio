// Copyright 2019 Istio Authors
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

package bootstrap

import (
	meshAPI "istio.io/api/mesh/v1alpha1"
)

// Config for generating an envoy bootstrap config file.
type Config struct {
	Epoch          int
	Proxy          *meshAPI.ProxyConfig
	ConfigOverride string
	PilotSAN       []string
	Node           string
	Options        map[string]interface{}
	NodeIPs        []string
	DNSRefreshRate string
	Env            []string
}
