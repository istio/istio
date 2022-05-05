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
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"

	extensions "istio.io/api/extensions/v1alpha1"
	"istio.io/istio/pkg/util/sets"
)

// Wasm header = magic number (4 bytes) + Wasm spec version (4 bytes).
var wasmHeader = append(wasmMagicNumber, []byte{0x1, 0x00, 0x00, 0x00}...)

func TestWasmCache(t *testing.T) {
	// Setup http server.
	tsNumRequest := int32(0)

	httpData := append(wasmHeader, []byte("data")...)
	invalidHTTPData := []byte("invalid binary")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tsNumRequest, 1)

		if r.URL.Path == "/different-url" {
			w.Write(append(httpData, []byte("different data")...))
		} else if r.URL.Path == "/invalid-wasm-header" {
			w.Write(invalidHTTPData)
		} else {
			w.Write(httpData)
		}
	}))
	defer ts.Close()
	httpDataSha := sha256.Sum256(httpData)
	httpDataCheckSum := hex.EncodeToString(httpDataSha[:])
	invalidHTTPDataSha := sha256.Sum256(invalidHTTPData)
	invalidHTTPDataCheckSum := hex.EncodeToString(invalidHTTPDataSha[:])

	reg := registry.New()
	// Set up a fake registry for OCI images.
	tos := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&tsNumRequest, 1)
		reg.ServeHTTP(w, r)
	}))
	defer tos.Close()
	ou, err := url.Parse(tos.URL)
	if err != nil {
		t.Fatal(err)
	}

	_, dockerImageDigest, invalidOCIImageDigest := setupOCIRegistry(t, ou.Host)

	ociWasmFile := fmt.Sprintf("%s.wasm", dockerImageDigest)
	ociURLWithTag := fmt.Sprintf("oci://%s/test/valid/docker:v0.1.0", ou.Host)
	ociURLWithLatestTag := fmt.Sprintf("oci://%s/test/valid/docker:latest", ou.Host)
	ociURLWithDigest := fmt.Sprintf("oci://%s/test/valid/docker@sha256:%s", ou.Host, dockerImageDigest)

	// Calculate cachehit sum.
	cacheHitSha := sha256.Sum256([]byte("cachehit"))
	cacheHitSum := hex.EncodeToString(cacheHitSha[:])

	cases := []struct {
		name                   string
		initialCachedModules   map[moduleKey]cacheEntry
		initialCachedChecksums map[string]*checksumEntry
		fetchURL               string
		purgeInterval          time.Duration
		wasmModuleExpiry       time.Duration
		checkPurgeTimeout      time.Duration
		checksum               string // Hex-encoded string.
		resourceName           string
		resourceVersion        string
		requestTimeout         time.Duration
		pullPolicy             extensions.PullPolicy
		wantFileName           string
		wantErrorMsgPrefix     string
		wantVisitServer        bool
		wantURLPurged          string
	}{
		{
			name:                   "cache miss",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL,
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			checksum:               httpDataCheckSum,
			requestTimeout:         time.Second * 10,
			wantFileName:           fmt.Sprintf("%s.wasm", httpDataCheckSum),
			wantVisitServer:        true,
		},
		{
			name: "cache hit",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ts.URL), checksum: cacheHitSum}: {modulePath: "test.wasm"},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL,
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			checksum:               cacheHitSum,
			requestTimeout:         time.Second * 10,
			wantFileName:           "test.wasm",
			wantVisitServer:        false,
		},
		{
			name:                   "invalid scheme",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               "foo://abc",
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			checksum:               httpDataCheckSum,
			requestTimeout:         time.Second * 10,
			wantFileName:           fmt.Sprintf("%s.wasm", httpDataCheckSum),
			wantErrorMsgPrefix:     "unsupported Wasm module downloading URL scheme: foo",
			wantVisitServer:        false,
		},
		{
			name:                   "download failure",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               "https://dummyurl",
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			requestTimeout:         time.Second * 10,
			wantErrorMsgPrefix:     "wasm module download failed after 5 attempts, last error: Get \"https://dummyurl\"",
			wantVisitServer:        false,
		},
		{
			name:                   "wrong checksum",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL,
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			checksum:               "wrongchecksum\n",
			requestTimeout:         time.Second * 10,
			wantErrorMsgPrefix:     fmt.Sprintf("module downloaded from %v has checksum %s, which does not match", ts.URL, httpDataCheckSum),
			wantVisitServer:        true,
		},
		{
			// this might be common error in user configuration, that url was updated, but not checksum.
			// Test that downloading still proceeds and error returns.
			name: "different url same checksum",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ts.URL), checksum: httpDataCheckSum}: {modulePath: fmt.Sprintf("%s.wasm", httpDataCheckSum)},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL + "/different-url",
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			checksum:               httpDataCheckSum,
			requestTimeout:         time.Second * 10,
			wantErrorMsgPrefix:     fmt.Sprintf("module downloaded from %v/different-url has checksum", ts.URL),
			wantVisitServer:        true,
		},
		{
			name: "invalid wasm header",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ts.URL), checksum: httpDataCheckSum}: {modulePath: fmt.Sprintf("%s.wasm", httpDataCheckSum)},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL + "/invalid-wasm-header",
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			checksum:               invalidHTTPDataCheckSum,
			requestTimeout:         time.Second * 10,
			wantErrorMsgPrefix:     fmt.Sprintf("fetched Wasm binary from %s is invalid", ts.URL+"/invalid-wasm-header"),
			wantVisitServer:        true,
		},
		{
			name: "purge on expiry",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ts.URL), checksum: httpDataCheckSum}: {modulePath: fmt.Sprintf("%s.wasm", httpDataCheckSum)},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL,
			purgeInterval:          1 * time.Millisecond,
			wasmModuleExpiry:       1 * time.Millisecond,
			checkPurgeTimeout:      5 * time.Second,
			checksum:               httpDataCheckSum,
			requestTimeout:         time.Second * 10,
			wantFileName:           fmt.Sprintf("%s.wasm", httpDataCheckSum),
			wantVisitServer:        true,
		},
		{
			name:                   "fetch oci without digest",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ociURLWithTag,
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			requestTimeout:         time.Second * 10,
			wantFileName:           ociWasmFile,
			wantVisitServer:        true,
		},
		{
			name:                   "fetch oci with digest",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ociURLWithTag,
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			requestTimeout:         time.Second * 10,
			checksum:               dockerImageDigest,
			wantFileName:           ociWasmFile,
			wantVisitServer:        true,
		},
		{
			name: "cache hit for tagged oci url with digest",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ociURLWithTag,
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			requestTimeout:         time.Second * 10,
			checksum:               dockerImageDigest,
			wantFileName:           ociWasmFile,
			wantVisitServer:        false,
		},
		{
			name: "cache hit for tagged oci url without digest",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			resourceName:     "namespace.resource",
			resourceVersion:  "0",
			fetchURL:         ociURLWithTag,
			purgeInterval:    DefaultWasmModulePurgeInterval,
			wasmModuleExpiry: DefaultWasmModuleExpiry,
			requestTimeout:   time.Second * 10,
			wantFileName:     ociWasmFile,
			wantVisitServer:  false,
		},
		{
			name: "cache miss for tagged oci url without digest",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ociURLWithTag,
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			requestTimeout:         time.Second * 10,
			wantFileName:           ociWasmFile,
			wantVisitServer:        true,
		},
		{
			name: "cache hit for oci url suffixed by digest",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ociURLWithDigest,
			purgeInterval:          DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:       DefaultWasmModuleExpiry,
			requestTimeout:         time.Second * 10,
			wantFileName:           ociWasmFile,
			wantVisitServer:        false,
		},
		{
			name: "pull due to pull-always policy when cache hit",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			resourceName:     "namespace.resource",
			resourceVersion:  "0",
			fetchURL:         ociURLWithTag,
			purgeInterval:    DefaultWasmModulePurgeInterval,
			wasmModuleExpiry: DefaultWasmModuleExpiry,
			requestTimeout:   time.Second * 10,
			pullPolicy:       extensions.PullPolicy_Always,
			wantFileName:     ociWasmFile,
			wantVisitServer:  true,
		},
		{
			name: "do not pull due to resourceVersion is the same",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			resourceName:     "namespace.resource",
			resourceVersion:  "123456",
			fetchURL:         ociURLWithTag,
			purgeInterval:    DefaultWasmModulePurgeInterval,
			wasmModuleExpiry: DefaultWasmModuleExpiry,
			requestTimeout:   time.Second * 10,
			pullPolicy:       extensions.PullPolicy_Always,
			wantFileName:     ociWasmFile,
			wantVisitServer:  false,
		},
		{
			name: "pull due to if-not-present policy when cache hit",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			resourceName:     "namespace.resource",
			resourceVersion:  "0",
			fetchURL:         ociURLWithTag,
			purgeInterval:    DefaultWasmModulePurgeInterval,
			wasmModuleExpiry: DefaultWasmModuleExpiry,
			requestTimeout:   time.Second * 10,
			pullPolicy:       extensions.PullPolicy_IfNotPresent,
			wantFileName:     ociWasmFile,
			wantVisitServer:  false,
		},
		{
			name: "do not pull in spite of pull-always policy due to checksum",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			fetchURL:         ociURLWithTag,
			purgeInterval:    DefaultWasmModulePurgeInterval,
			wasmModuleExpiry: DefaultWasmModuleExpiry,
			requestTimeout:   time.Second * 10,
			checksum:         dockerImageDigest,
			pullPolicy:       extensions.PullPolicy_Always,
			wantFileName:     ociWasmFile,
			wantVisitServer:  false,
		},
		{
			name: "do not pull in spite of latest tag due to checksum",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithLatestTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			fetchURL:         ociURLWithLatestTag,
			purgeInterval:    DefaultWasmModulePurgeInterval,
			wasmModuleExpiry: DefaultWasmModuleExpiry,
			requestTimeout:   time.Second * 10,
			checksum:         dockerImageDigest,
			pullPolicy:       extensions.PullPolicy_UNSPECIFIED_POLICY, // Default policy
			wantFileName:     ociWasmFile,
			wantVisitServer:  false,
		},
		{
			name: "do not pull in spite of latest tag due to IfNotPresent policy",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithLatestTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			fetchURL:         ociURLWithLatestTag,
			purgeInterval:    DefaultWasmModulePurgeInterval,
			wasmModuleExpiry: DefaultWasmModuleExpiry,
			requestTimeout:   time.Second * 10,
			checksum:         dockerImageDigest,
			pullPolicy:       extensions.PullPolicy_IfNotPresent,
			wantFileName:     ociWasmFile,
			wantVisitServer:  false,
		},
		{
			name: "purge OCI image on expiry",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: urlAsResourceName(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile, referencingURLs: sets.New(ociURLWithTag)},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource1": "123456",
					},
				},
				"test-url": {
					checksum: "test-checksum",
					resourceVersionByResource: map[string]string{
						"namespace.resource2": "123456",
					},
				},
			},
			fetchURL:          ociURLWithDigest,
			purgeInterval:     1 * time.Millisecond,
			wasmModuleExpiry:  1 * time.Millisecond,
			requestTimeout:    time.Second * 10,
			checkPurgeTimeout: 5 * time.Second,
			wantFileName:      ociWasmFile,
			wantVisitServer:   true,
			wantURLPurged:     ociURLWithTag,
		},
		{
			name:                 "fetch oci timed out",
			initialCachedModules: map[moduleKey]cacheEntry{},
			fetchURL:             ociURLWithTag,
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			requestTimeout:       0, // Cause timeout immediately.
			wantErrorMsgPrefix:   fmt.Sprintf("could not fetch Wasm OCI image: could not fetch manifest: Get \"https://%s/v2/\"", ou.Host),
			wantVisitServer:      false,
		},
		{
			name:                 "fetch oci with wrong digest",
			initialCachedModules: map[moduleKey]cacheEntry{},
			fetchURL:             ociURLWithTag,
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			requestTimeout:       time.Second * 10,
			checksum:             "wrongdigest",
			wantErrorMsgPrefix: fmt.Sprintf(
				"module downloaded from %v has checksum %v, which does not match:", fmt.Sprintf("oci://%s/test/valid/docker:v0.1.0", ou.Host), dockerImageDigest,
			),
			wantVisitServer: true,
		},
		{
			name:                 "fetch invalid oci",
			initialCachedModules: map[moduleKey]cacheEntry{},
			fetchURL:             fmt.Sprintf("oci://%s/test/invalid", ou.Host),
			purgeInterval:        DefaultWasmModulePurgeInterval,
			wasmModuleExpiry:     DefaultWasmModuleExpiry,
			checksum:             invalidOCIImageDigest,
			requestTimeout:       time.Second * 10,
			wantErrorMsgPrefix: `could not fetch Wasm binary: the given image is in invalid format as an OCI image: 2 errors occurred:
	* could not parse as compat variant: invalid media type application/vnd.oci.image.layer.v1.tar (expect application/vnd.oci.image.layer.v1.tar+gzip)
	* could not parse as oci variant: number of layers must be 2 but got 1`,
			wantVisitServer: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			cache := NewLocalFileCache(tmpDir, c.purgeInterval, c.wasmModuleExpiry, nil)
			cache.httpFetcher.initialBackoff = time.Microsecond
			defer close(cache.stopChan)

			var cacheHitKey *moduleKey
			initTime := time.Now()
			cache.mux.Lock()
			for k, m := range c.initialCachedModules {
				filePath := filepath.Join(tmpDir, m.modulePath)
				err := os.WriteFile(filePath, []byte("data/\n"), 0o644)
				if err != nil {
					t.Fatalf("failed to write initial wasm module file %v", err)
				}
				mkey := moduleKey{name: k.name, checksum: k.checksum}

				cache.modules[mkey] = &cacheEntry{modulePath: filePath, last: initTime}
				if m.referencingURLs != nil {
					cache.modules[mkey].referencingURLs = m.referencingURLs.Copy()
				} else {
					cache.modules[mkey].referencingURLs = sets.New()
				}

				if urlAsResourceName(c.fetchURL) == k.name && c.checksum == k.checksum {
					cacheHitKey = &mkey
				}
			}

			for k, m := range c.initialCachedChecksums {
				cache.checksums[k] = m
			}
			cache.mux.Unlock()

			if c.checkPurgeTimeout > 0 {
				moduleDeleted := false
				for start := time.Now(); time.Since(start) < c.checkPurgeTimeout; {
					// Check existence of module files. files should be deleted before timing out.
					if files, err := os.ReadDir(tmpDir); err == nil && len(files) == 0 {
						moduleDeleted = true
						break
					}
				}

				cache.mux.Lock()
				_, ok := cache.checksums[c.wantURLPurged]
				cache.mux.Unlock()
				if ok {
					t.Fatalf("the checksum cache for %v is not purged before purge timeout", c.wantURLPurged)
				}

				if !moduleDeleted {
					t.Fatalf("Wasm modules are not purged before purge timeout")
				}
			}

			atomic.StoreInt32(&tsNumRequest, 0)
			gotFilePath, gotErr := cache.Get(c.fetchURL, c.checksum, c.resourceName, c.resourceVersion, c.requestTimeout, []byte{}, c.pullPolicy)
			serverVisited := atomic.LoadInt32(&tsNumRequest) > 0

			if cacheHitKey != nil {
				cache.mux.Lock()
				if entry, ok := cache.modules[*cacheHitKey]; ok && entry.last == initTime {
					t.Errorf("Wasm module cache entry's last access time not updated after get operation, key: %v", *cacheHitKey)
				}
				cache.mux.Unlock()
			}
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
			if c.wantVisitServer != serverVisited {
				t.Errorf("test wasm binary server encountered the unexpected visiting status got %v, want %v", serverVisited, c.wantVisitServer)
			}
		})
	}
}

func setupOCIRegistry(t *testing.T, host string) (wantBinaryCheckSum, dockerImageDigest, invalidOCIImageDigest string) {
	// Push *compat* variant docker image (others are well tested in imagefetcher's test and the behavior is consistent).
	ref := fmt.Sprintf("%s/test/valid/docker:v0.1.0", host)
	binary := append(wasmHeader, []byte("this is wasm plugin")...)

	// Create docker layer.
	l, err := newMockLayer(types.DockerLayer,
		map[string][]byte{"plugin.wasm": binary})
	if err != nil {
		t.Fatal(err)
	}
	img, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
	if err != nil {
		t.Fatal(err)
	}

	// Set manifest type.
	manifest, err := img.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	manifest.MediaType = types.DockerManifestSchema2

	// Push image to the registry.
	err = crane.Push(img, ref)
	if err != nil {
		t.Fatal(err)
	}

	// Push image to the registry with latest tag as well
	ref = fmt.Sprintf("%s/test/valid/docker:latest", host)
	err = crane.Push(img, ref)
	if err != nil {
		t.Fatal(err)
	}

	// Calculate sum
	sha := sha256.Sum256(binary)
	wantBinaryCheckSum = hex.EncodeToString(sha[:])
	d, _ := img.Digest()
	dockerImageDigest = d.Hex

	// Finally push the invalid image.
	ref = fmt.Sprintf("%s/test/invalid", host)
	l, err = newMockLayer(types.OCIUncompressedLayer, map[string][]byte{"not-wasm.txt": []byte("a")})
	if err != nil {
		t.Fatal(err)
	}
	img2, err := mutate.Append(empty.Image, mutate.Addendum{Layer: l})
	if err != nil {
		t.Fatal(err)
	}

	// Set manifest type so it will pass the docker parsing branch.
	img2 = mutate.MediaType(img2, types.OCIManifestSchema1)

	d, _ = img2.Digest()
	invalidOCIImageDigest = d.Hex

	// Push image to the registry.
	err = crane.Push(img2, ref)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestWasmCacheMissChecksum(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewLocalFileCache(tmpDir, DefaultWasmModulePurgeInterval, DefaultWasmModuleExpiry, nil)
	defer close(cache.stopChan)

	gotNumRequest := 0
	binary1 := append(wasmHeader, 1)
	binary2 := append(wasmHeader, 2)
	// Create a test server which returns 0 for the first two calls, and returns 1 for the following calls.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotNumRequest <= 1 {
			w.Write(binary1)
		} else {
			w.Write(binary2)
		}
		gotNumRequest++
	}))
	defer ts.Close()
	wantFilePath1 := filepath.Join(tmpDir, fmt.Sprintf("%x.wasm", sha256.Sum256(binary1)))
	wantFilePath2 := filepath.Join(tmpDir, fmt.Sprintf("%x.wasm", sha256.Sum256(binary2)))
	var defaultPullPolicy extensions.PullPolicy

	// Get wasm module three times, since checksum is not specified, it will be fetched from module server every time.
	// 1st time
	gotFilePath, err := cache.Get(ts.URL, "", "namespace.resource", "123456", time.Second*10, []byte{}, defaultPullPolicy)
	if err != nil {
		t.Fatalf("failed to download Wasm module: %v", err)
	}
	if gotFilePath != wantFilePath1 {
		t.Errorf("wasm download path got %v want %v", gotFilePath, wantFilePath1)
	}

	// 2nd time
	gotFilePath, err = cache.Get(ts.URL, "", "namespace.resource", "123456", time.Second*10, []byte{}, defaultPullPolicy)
	if err != nil {
		t.Fatalf("failed to download Wasm module: %v", err)
	}
	if gotFilePath != wantFilePath1 {
		t.Errorf("wasm download path got %v want %v", gotFilePath, wantFilePath1)
	}

	// 3rd time
	gotFilePath, err = cache.Get(ts.URL, "", "namespace.resource", "123456", time.Second*10, []byte{}, defaultPullPolicy)
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
