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

	"istio.io/istio/tests/util/leak"
)

func TestMain(m *testing.M) {
	leak.CheckMain(m)
}

func TestWasmCache(t *testing.T) {
	var tsNumRequest int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tsNumRequest++
		fmt.Fprintln(w, "data"+r.URL.Path)
	}))
	defer ts.Close()
	dataCheckSum := sha256.Sum256([]byte("data/\n"))

	cases := []struct {
		name                 string
		initialCachedModules map[cacheKey]cacheEntry
		fetchURL             string
		purgeInterval        time.Duration
		wasmModuleExpiry     time.Duration
		checkPurgeTimeout    time.Duration
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
			wantErrorMsgPrefix:   "wasm module download failed, last error: Get \"https://dummyurl\"",
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
			fetchURL:          ts.URL,
			purgeInterval:     1 * time.Millisecond,
			wasmModuleExpiry:  1 * time.Millisecond,
			checkPurgeTimeout: 5 * time.Second,
			checksum:          dataCheckSum,
			wantFileName:      fmt.Sprintf("%x.wasm", dataCheckSum),
			wantServerReqNum:  1,
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

			if c.checkPurgeTimeout > 0 {
				moduleDeleted := false
				for start := time.Now(); time.Since(start) < c.checkPurgeTimeout; {
					// Check existence of module files. files should be deleted before timing out.
					if files, err := ioutil.ReadDir(tmpDir); err == nil && len(files) == 0 {
						moduleDeleted = true
						break
					}
				}
				if !moduleDeleted {
					t.Fatalf("Wasm modules are not purged before purge timeout")
				}
			}

			gotFilePath, gotErr := cache.Get(c.fetchURL, fmt.Sprintf("%x", c.checksum), 0)
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
				t.Errorf("test server request number got %v, want %v", tsNumRequest, c.wantServerReqNum)
			}
		})
	}
}

func TestWasmCacheMissChecksum(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewLocalFileCache(tmpDir, DefaultWasmModulePurgeInteval, DefaultWasmModuleExpiry)
	defer close(cache.stopChan)

	gotNumRequest := 0
	// Create a test server which returns 0 for the first two calls, and returns 1 for the following calls.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotNumRequest <= 1 {
			fmt.Fprintln(w, "0")
		} else {
			fmt.Fprintln(w, "1")
		}
		gotNumRequest++
	}))
	defer ts.Close()
	wantFilePath1 := filepath.Join(tmpDir, fmt.Sprintf("%x.wasm", sha256.Sum256([]byte("0\n"))))
	wantFilePath2 := filepath.Join(tmpDir, fmt.Sprintf("%x.wasm", sha256.Sum256([]byte("1\n"))))

	// Get wasm module three times, since checksum is not specified, it will be fetched from module server every time.
	// 1st time
	gotFilePath, err := cache.Get(ts.URL, "", 0)
	if err != nil {
		t.Fatalf("failed to download Wasm module: %v", err)
	}
	if gotFilePath != wantFilePath1 {
		t.Errorf("wasm download path got %v want %v", gotFilePath, wantFilePath1)
	}

	// 2nd time
	gotFilePath, err = cache.Get(ts.URL, "", 0)
	if err != nil {
		t.Fatalf("failed to download Wasm module: %v", err)
	}
	if gotFilePath != wantFilePath1 {
		t.Errorf("wasm download path got %v want %v", gotFilePath, wantFilePath1)
	}

	// 3rd time
	gotFilePath, err = cache.Get(ts.URL, "", 0)
	if err != nil {
		t.Fatalf("failed to download Wasm module: %v", err)
	}
	if gotFilePath != wantFilePath2 {
		t.Errorf("wasm download path got %v want %v", gotFilePath, wantFilePath2)
	}

	wantNumRequest := 3
	if gotNumRequest != wantNumRequest {
		t.Errorf("wasm download call got %v want %v", gotNumRequest, wantNumRequest)
	}
}
