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

package google

import (
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/wasm/keychain"

	"github.com/google/go-containerregistry/pkg/v1/google"
)

func init() {
	// In google.Keychain, the credential is cached by ReuseTokenSource.
	// Refer to https://github.com/google/go-containerregistry/blob/4d7b65b04609719eb0f23afa8669ba4b47178571/pkg/v1/google/auth.go#L60.
	keychain.RegisterKeychain(platform.PlatformTypeGCP, google.Keychain)
}
