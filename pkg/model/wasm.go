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

package model

import (
	"istio.io/istio/pkg/wellknown"
)

const (
	// name of environment variable at Wasm VM, which will carry the Wasm image pull secret.
	WasmSecretEnv = "ISTIO_META_WASM_IMAGE_PULL_SECRET"
	// name of environment variable at Wasm VM, which will carry the Wasm image pull policy.
	WasmPolicyEnv = "ISTIO_META_WASM_IMAGE_PULL_POLICY"
	// name of environment variable at Wasm VM, which will carry the resource version of WasmPlugin.
	WasmResourceVersionEnv = "ISTIO_META_WASM_PLUGIN_RESOURCE_VERSION"

	WasmHTTPFilterType    = APITypePrefix + wellknown.HTTPWasm
	WasmNetworkFilterType = APITypePrefix + "envoy.extensions.filters.network.wasm.v3.Wasm"
	TypedStructType       = APITypePrefix + "udpa.type.v1.TypedStruct"
)
