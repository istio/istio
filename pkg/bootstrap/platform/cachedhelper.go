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

package platform

import (
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
)

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

func wrapHelperWithCache(helper authn.Helper, expirationInterval time.Duration) authn.Helper {
	return &cachedHelper{
		internalHelper: helper,
		expiration:     expirationInterval,
		cache:          make(map[string]cachedHelperEntry),
		getNow:         time.Now,
	}
}
