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

package perf

// Config is the Mixer server configuration to use during perf tests.
// TODO: We should ideally combine this file with pkg/server/Args. Unfortunately, pkg/serverArgs is not serializable.
type Config struct {
	Global         string `json:"global"`
	Service        string `json:"rpcServer"`
	EnableLog      bool   `json:"enableLog,omitempty"`
	EnableDebugLog bool   `json:"enableDebugLog,omitempty"`
	SingleThreaded bool   `json:"singleThreaded,omitempty"`

	// Templates is the name of the templates to use in this test. If left empty, a standard set of templates
	// will be used.
	Templates []string `json:"templates,omitempty"`

	// Adapters is the name of the adapters to use for this test. If left empty, a standard set of adapters
	// will be used.
	Adapters []string `json:"adapters,omitempty"`
}
