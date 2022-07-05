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

package azure

import (
	"time"

	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/wasm/keychain"

	acr "github.com/chrismellard/docker-credential-acr-env/pkg/credhelper"
	"github.com/google/go-containerregistry/pkg/authn"
)

const (
	// According to https://docs.microsoft.com/en-us/azure/active-directory/develop/refresh-tokens#refresh-token-lifetime,
	// the expiration interval of the auth token in Azure is 24 hour. For safety, let's uses 12 hours.
	// Note that Azure uses the Azure AD refresh token as a password when authenticating a docker client.
	acrCredExpiration = time.Hour * 12
)

func init() {
	// ACR helpers does not provide simple way to cache the credential before expiration at this moment.
	// So, `cachedHelper` keeps the credential for the specified duration.
	keychain.RegisterKeychain(platform.PlatformTypeAzure, authn.NewMultiKeychain(
		authn.DefaultKeychain,
		authn.NewKeychainFromHelper(
			keychain.WrapHelperWithCache(acr.NewACRCredentialsHelper(), acrCredExpiration))))
}
