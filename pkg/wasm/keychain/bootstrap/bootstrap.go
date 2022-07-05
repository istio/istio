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

package bootstrap

import (
	// Load and register the AWS keychain
	_ "istio.io/istio/pkg/wasm/keychain/aws"
	// Load and register the Azure keychain
	_ "istio.io/istio/pkg/wasm/keychain/azure"
	// Load and register the Google keychain
	_ "istio.io/istio/pkg/wasm/keychain/google"
)
