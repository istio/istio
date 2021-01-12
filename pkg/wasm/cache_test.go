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
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWasmCache(t *testing.T) {
	var tsNumRequest int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tsNumRequest++
		fmt.Fprintln(w, "data"+r.URL.Path)
	}))
	dataCheckSum := sha256.Sum256([]byte("data/\n"))

	cases := []struct {
		name                 string
		initialCachedModules map[cacheKey]cacheEntry
		fetchURL             string
		purgeInterval        time.Duration
		wasmModuleExpiry     time.Duration
		checksum             [32]byte
		wantFileName         string
		wantErrorMsgPrefix   string
		wantServerReqNum     int
	}{
		{
			name:                 "cache miss",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             ts.URL,
			purgeInterval:        DefaultWasmModulePurgeInteval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			checksum:             dataCheckSum,
			wantFileName:         fmt.Sprintf("%x.wasm", dataCheckSum),
			wantServerReqNum:     1,
		},
		{
			name: "cache hit",
			initialCachedModules: map[cacheKey]cacheEntry{
				{downloadURL: ts.URL, checksum: fmt.Sprintf("%x", sha256.Sum256([]byte("cachehit\n")))}: {modulePath: "test.wasm"},
			},
			fetchURL:         ts.URL,
			purgeInterval:    DefaultWasmModulePurgeInteval,
			wasmModuleExpiry: DefaultWasmModuleExpiry,
			checksum:         sha256.Sum256([]byte("cachehit\n")),
			wantFileName:     "test.wasm",
			wantServerReqNum: 0,
		},
		{
			name:                 "invalid scheme",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             "oci://abc",
			purgeInterval:        DefaultWasmModulePurgeInteval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			checksum:             dataCheckSum,
			wantFileName:         fmt.Sprintf("%x.wasm", dataCheckSum),
			wantErrorMsgPrefix:   "unsupported Wasm module downloading URL scheme: oci",
			wantServerReqNum:     0,
		},
		{
			name:                 "download failure",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             "https://dummyurl",
			purgeInterval:        DefaultWasmModulePurgeInteval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			wantErrorMsgPrefix:   "Wasm module download failed, last error: Get \"https://dummyurl\"",
			wantServerReqNum:     0,
		},
		{

			name:                 "wrong checksum",
			initialCachedModules: map[cacheKey]cacheEntry{},
			fetchURL:             ts.URL,
			purgeInterval:        DefaultWasmModulePurgeInteval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			checksum:             sha256.Sum256([]byte("wrongchecksum\n")),
			wantErrorMsgPrefix:   fmt.Sprintf("module downloaded from %v has checksum %x, which does not match", ts.URL, dataCheckSum),
			wantServerReqNum:     1,
		},
		{
			// this might be common error in user configuration, that url was updated, but not checksum.
			// Test that downloading still proceeds and error returns.
			name: "different url same checksum",
			initialCachedModules: map[cacheKey]cacheEntry{
				{downloadURL: ts.URL, checksum: fmt.Sprintf("%x", dataCheckSum)}: {modulePath: fmt.Sprintf("%x.wasm", dataCheckSum)},
			},
			fetchURL:           ts.URL + "/different-url",
			purgeInterval:      DefaultWasmModulePurgeInteval,
			wasmModuleExpiry:   DefaultWasmModuleExpiry,
			checksum:           dataCheckSum,
			wantErrorMsgPrefix: fmt.Sprintf("module downloaded from %v/different-url has checksum", ts.URL),
			wantServerReqNum:   1,
		},
		{
			name: "purge on expiry",
			initialCachedModules: map[cacheKey]cacheEntry{
				{downloadURL: ts.URL, checksum: fmt.Sprintf("%x", dataCheckSum)}: {modulePath: fmt.Sprintf("%x.wasm", dataCheckSum)},
			},
			fetchURL:         ts.URL,
			purgeInterval:    1 * time.Millisecond,
			wasmModuleExpiry: 1 * time.Millisecond,
			checksum:         dataCheckSum,
			wantFileName:     fmt.Sprintf("%x.wasm", dataCheckSum),
			wantServerReqNum: 1,
		},
		{
			name: "cache hit before purge",
			initialCachedModules: map[cacheKey]cacheEntry{
				{downloadURL: ts.URL, checksum: fmt.Sprintf("%x", dataCheckSum)}: {modulePath: fmt.Sprintf("%x.wasm", dataCheckSum)},
			},
			fetchURL:         ts.URL,
			purgeInterval:    3 * time.Millisecond,
			wasmModuleExpiry: 3 * time.Millisecond,
			checksum:         dataCheckSum,
			wantFileName:     fmt.Sprintf("%x.wasm", dataCheckSum),
			wantServerReqNum: 0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cache := NewLocalFileCache(tmpDir, c.purgeInterval, c.wasmModuleExpiry)
			defer close(cache.stopChan)
			tsNumRequest = 0

			cache.mux.Lock()
			for k, m := range c.initialCachedModules {
				filePath := filepath.Join(tmpDir, m.modulePath)
				err := ioutil.WriteFile(filePath, []byte("data/\n"), 0644)
				if err != nil {
					t.Fatalf("failed to write initial wasm module file %v", err)
				}
				cache.modules[cacheKey{downloadURL: k.downloadURL, checksum: k.checksum}] =
					cacheEntry{modulePath: filePath, last: time.Now()}
			}
			cache.mux.Unlock()

			// Sleep 2 ms for purge on expiry testing
			time.Sleep(2 * time.Millisecond)

			gotFilePath, gotErr := cache.Get(c.fetchURL, fmt.Sprintf("%x", c.checksum), nil)
			wantFilePath := filepath.Join(tmpDir, c.wantFileName)
			if c.wantErrorMsgPrefix != "" {
				if gotErr == nil {
					t.Errorf("Wasm module cache lookup got no error, want error prefix `%v`", c.wantErrorMsgPrefix)
				} else if !strings.HasPrefix(gotErr.Error(), c.wantErrorMsgPrefix) {
					t.Errorf("Wasm module cache lookup got error `%v`, want error prefix `%v`", gotErr, c.wantErrorMsgPrefix)
				}
			} else if gotFilePath != wantFilePath {
				t.Errorf("Wasm module local file path got %v, want %v", gotFilePath, wantFilePath)
				if gotErr != nil {
					t.Errorf("got unexpected error %v", gotErr)
				}
			}
			if c.wantServerReqNum != tsNumRequest {
				t.Errorf("test server request number got %v, want %v", c.wantServerReqNum, tsNumRequest)
			}
		})
	}
}
