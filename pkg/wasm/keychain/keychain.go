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

package keychain

import (
	"time"

	"istio.io/istio/pkg/bootstrap/platform"

	"github.com/google/go-containerregistry/pkg/authn"
)

var registeredKeychains = map[platform.PlatformType]authn.Keychain{
	platform.PlatformTypeNone: authn.DefaultKeychain,
}

// Registers the given key chain with the platform type.
// To take account for the default keychain always, prepend the default keychain.
func RegisterKeychain(t platform.PlatformType, k authn.Keychain) {
	registeredKeychains[t] = authn.NewMultiKeychain(authn.DefaultKeychain, k)
}

// Returns a key chain with the support for vendor specific keychain by the given platform type.
func GetPlatformSpecificKeyChain(platformType platform.PlatformType) authn.Keychain {
	if keychain, ok := registeredKeychains[platformType]; ok {
		return keychain
	}
	return authn.DefaultKeychain
}

// GetRegisteredKeychainsCount returns the number of registered keychains.
// This is just used for testing at this moment.
func GetRegisteredKeychainsCount() int {
	return len(registeredKeychains)
}

type cachedHelperEntry struct {
	username string
	password string
	expireAt time.Time
}
type cachedHelper struct {
	internalHelper authn.Helper
	expiration     time.Duration
	cache          map[string]cachedHelperEntry
	getNow         func() time.Time
}

func (helper *cachedHelper) Get(serverURL string) (string, string, error) {
	entry, ok := helper.cache[serverURL]
	now := helper.getNow()
	if !ok || now.After(entry.expireAt) {
		username, password, err := helper.internalHelper.Get(serverURL)
		if err != nil {
			delete(helper.cache, serverURL)
			return "", "", err
		}
		entry = cachedHelperEntry{
			username: username,
			password: password,
			expireAt: now.Add(helper.expiration),
		}
		helper.cache[serverURL] = entry
	}
	return entry.username, entry.password, nil
}

func WrapHelperWithCache(helper authn.Helper, expirationInterval time.Duration) authn.Helper {
	return &cachedHelper{
		internalHelper: helper,
		expiration:     expirationInterval,
		cache:          make(map[string]cachedHelperEntry),
		getNow:         time.Now,
	}
}
