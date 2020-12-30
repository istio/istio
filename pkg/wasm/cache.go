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

package wasm

import (
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"net/url"
	"path/filepath"
	"sync"
	"time"
)

type Cache struct {
	// Sha256 to local file path which contains the Wasm module.
	modules     map[string]cacheEntry
	httpFetcher *HTTPFetcher
	dir         string

	mux sync.Mutex
}

type cacheEntry struct {
	modulePath string
	last       time.Time
}

func NewCache(dir string) *Cache {
	cache := &Cache{
		httpFetcher: NewHTTPFetcher(),
		modules:     make(map[string]cacheEntry),
		dir:         dir,
	}
	go func() {
		// TODO: purge on TTL
	}()
	return cache
}

func (c *Cache) Get(fetchURL, sha string) (string, error) {
	wasmURL, err := url.Parse(fetchURL)
	if err != nil {
		return "", fmt.Errorf("fail to parse Wasm module fetch url: %s", fetchURL)
	}
	switch wasmURL.Scheme {
	case "http", "https":
		// Check cache
		if ce, ok := c.modules[sha]; ok {
			ce.last = time.Now()
			return ce.modulePath, nil
		}

		// If not found, fetch remotely
		b, err := c.httpFetcher.Fetch(fetchURL)
		if err != nil {
			return "", err
		}
		// Check sha256sum
		ds := sha256.Sum256(b)
		if fmt.Sprintf("%x", ds) != sha {
			return "", fmt.Errorf("module check sum does not: %s", fetchURL)
		}
		// Materialize the Wasm module into a local file
		f := filepath.Join(c.dir, fmt.Sprintf("%s.wasm", sha))
		err = ioutil.WriteFile(f, b, 0644)
		if err != nil {
			return "", err
		}

		// Populate cache
		ce := cacheEntry{
			modulePath: f,
			last:       time.Now(),
		}
		c.modules[sha] = ce

		return f, nil
	}
	return "", nil
}
