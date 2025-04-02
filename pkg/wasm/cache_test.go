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
	"crypto/tls"
	"encoding/hex"
	"errors"
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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"

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

	dockerImageDigest, invalidOCIImageDigest := setupOCIRegistry(t, ou.Host)

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
		getOptions             GetOptions
		wantCachedModules      map[moduleKey]*cacheEntry
		wantCachedChecksums    map[string]*checksumEntry
		wantFileName           string
		wantErrorMsgPrefix     string
		wantVisitServer        bool
	}{
		{
			name:                   "cache miss",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL,
			getOptions: GetOptions{
				Checksum:        httpDataCheckSum,
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: ts.URL, checksum: httpDataCheckSum}: {modulePath: httpDataCheckSum + ".wasm"},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ts.URL: {checksum: httpDataCheckSum, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    fmt.Sprintf("%s.wasm", httpDataCheckSum),
			wantVisitServer: true,
		},
		{
			name: "cache hit",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ts.URL), checksum: cacheHitSum}: {modulePath: "test.wasm"},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL,
			getOptions: GetOptions{
				Checksum:        cacheHitSum,
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: ts.URL, checksum: cacheHitSum}: {modulePath: "test.wasm"},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ts.URL: {checksum: cacheHitSum, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    "test.wasm",
			wantVisitServer: false,
		},
		{
			name:                   "invalid scheme",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               "foo://abc",
			getOptions: GetOptions{
				Checksum:       httpDataCheckSum,
				RequestTimeout: time.Second * 10,
			},
			wantCachedModules:   map[moduleKey]*cacheEntry{},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantFileName:        fmt.Sprintf("%s.wasm", httpDataCheckSum),
			wantErrorMsgPrefix:  "unsupported Wasm module downloading URL scheme: foo",
			wantVisitServer:     false,
		},
		{
			name:                   "download failure",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               "https://-invalid-url",
			getOptions: GetOptions{
				RequestTimeout: time.Second * 10,
			},
			wantCachedModules:   map[moduleKey]*cacheEntry{},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantErrorMsgPrefix:  "wasm module download failed after 5 attempts, last error: Get \"https://-invalid-url\"",
			wantVisitServer:     false,
		},
		{
			name:                   "wrong checksum",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL,
			getOptions: GetOptions{
				Checksum:       "wrongchecksum\n",
				RequestTimeout: time.Second * 10,
			},
			wantCachedModules:   map[moduleKey]*cacheEntry{},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantErrorMsgPrefix:  fmt.Sprintf("module downloaded from %v has checksum %s, which does not match", ts.URL, httpDataCheckSum),
			wantVisitServer:     true,
		},
		{
			// this might be common error in user configuration, that url was updated, but not checksum.
			// Test that downloading still proceeds and error returns.
			name: "different url same checksum",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ts.URL), checksum: httpDataCheckSum}: {modulePath: fmt.Sprintf("%s.wasm", httpDataCheckSum)},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL + "/different-url",
			getOptions: GetOptions{
				Checksum:       httpDataCheckSum,
				RequestTimeout: time.Second * 10,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: ts.URL, checksum: httpDataCheckSum}: {modulePath: httpDataCheckSum + ".wasm"},
			},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantErrorMsgPrefix:  fmt.Sprintf("module downloaded from %v/different-url has checksum", ts.URL),
			wantVisitServer:     true,
		},
		{
			name: "invalid wasm header",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ts.URL), checksum: httpDataCheckSum}: {modulePath: fmt.Sprintf("%s.wasm", httpDataCheckSum)},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL + "/invalid-wasm-header",
			getOptions: GetOptions{
				Checksum:       invalidHTTPDataCheckSum,
				RequestTimeout: time.Second * 10,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: ts.URL, checksum: httpDataCheckSum}: {modulePath: httpDataCheckSum + ".wasm"},
			},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantErrorMsgPrefix:  fmt.Sprintf("fetched Wasm binary from %s is invalid", ts.URL+"/invalid-wasm-header"),
			wantVisitServer:     true,
		},
		{
			name:                   "purge on expiry",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ts.URL,
			purgeInterval:          1 * time.Millisecond,
			wasmModuleExpiry:       1 * time.Millisecond,
			checkPurgeTimeout:      5 * time.Second,
			getOptions: GetOptions{
				Checksum:       httpDataCheckSum,
				RequestTimeout: time.Second * 10,
			},
			wantCachedModules:   map[moduleKey]*cacheEntry{},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantFileName:        fmt.Sprintf("%s.wasm", httpDataCheckSum),
			wantVisitServer:     true,
		},
		{
			name:                   "fetch oci without digest",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ociURLWithTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: true,
		},
		{
			name:                   "fetch oci with digest",
			initialCachedModules:   map[moduleKey]cacheEntry{},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ociURLWithTag,
			getOptions: GetOptions{
				Checksum:        dockerImageDigest,
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: true,
		},
		{
			name: "cache hit for tagged oci url with digest",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ociURLWithTag,
			getOptions: GetOptions{
				Checksum:        dockerImageDigest,
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: false,
		},
		{
			name: "cache hit for tagged oci url without digest",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			fetchURL: ociURLWithTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: false,
		},
		{
			name: "cache miss for tagged oci url without digest",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
			},
			fetchURL: ociURLWithTag,
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: true,
		},
		{
			name: "cache hit for oci url suffixed by digest",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{},
			fetchURL:               ociURLWithDigest,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantFileName:        ociWasmFile,
			wantVisitServer:     false,
		},
		{
			name: "pull due to pull-always policy when cache hit",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			fetchURL: ociURLWithTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
				PullPolicy:      Always,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: true,
		},
		{
			name: "do not pull due to resourceVersion is the same",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			fetchURL: ociURLWithTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "123456",
				RequestTimeout:  time.Second * 10,
				PullPolicy:      Always,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "123456"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: false,
		},
		{
			name: "pull due to if-not-present policy when cache hit",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			fetchURL: ociURLWithTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
				PullPolicy:      IfNotPresent,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: false,
		},
		{
			name: "do not pull in spite of pull-always policy due to checksum",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			fetchURL: ociURLWithTag,
			getOptions: GetOptions{
				Checksum:        dockerImageDigest,
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
				PullPolicy:      Always,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: false,
		},
		{
			name: "pull due to latest tag",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithLatestTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithLatestTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			fetchURL: ociURLWithLatestTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
				PullPolicy:      Unspecified, // Default policy
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithLatestTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithLatestTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: true,
		},
		{
			name: "do not pull in spite of latest tag due to checksum",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithLatestTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithLatestTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			fetchURL: ociURLWithLatestTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
				Checksum:        dockerImageDigest,
				PullPolicy:      Unspecified, // Default policy
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithLatestTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithLatestTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: false,
		},
		{
			name: "do not pull in spite of latest tag due to IfNotPresent policy",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithLatestTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithLatestTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
			},
			fetchURL: ociURLWithLatestTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
				PullPolicy:      IfNotPresent,
			},
			wantCachedModules: map[moduleKey]*cacheEntry{
				{name: moduleNameFromURL(ociURLWithLatestTag), checksum: dockerImageDigest}: {modulePath: ociWasmFile},
			},
			wantCachedChecksums: map[string]*checksumEntry{
				ociURLWithLatestTag: {checksum: dockerImageDigest, resourceVersionByResource: map[string]string{"namespace.resource": "0"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: false,
		},
		{
			name: "purge OCI image on expiry",
			initialCachedModules: map[moduleKey]cacheEntry{
				{name: moduleNameFromURL(ociURLWithTag) + "-purged", checksum: dockerImageDigest}: {
					modulePath:      ociWasmFile,
					referencingURLs: sets.New(ociURLWithTag),
				},
			},
			initialCachedChecksums: map[string]*checksumEntry{
				ociURLWithTag: {
					checksum: dockerImageDigest,
					resourceVersionByResource: map[string]string{
						"namespace.resource": "123456",
					},
				},
				"test-url": {
					checksum: "test-checksum",
					resourceVersionByResource: map[string]string{
						"namespace.resource2": "123456",
					},
				},
			},
			fetchURL:         ociURLWithDigest,
			purgeInterval:    1 * time.Millisecond,
			wasmModuleExpiry: 1 * time.Millisecond,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
			},
			checkPurgeTimeout: 5 * time.Second,
			wantCachedModules: map[moduleKey]*cacheEntry{},
			wantCachedChecksums: map[string]*checksumEntry{
				"test-url": {checksum: "test-checksum", resourceVersionByResource: map[string]string{"namespace.resource2": "123456"}},
			},
			wantFileName:    ociWasmFile,
			wantVisitServer: true,
		},
		{
			name:                 "fetch oci timed out",
			initialCachedModules: map[moduleKey]cacheEntry{},
			fetchURL:             ociURLWithTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  0, // Cause timeout immediately.
			},
			wantCachedModules:   map[moduleKey]*cacheEntry{},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantErrorMsgPrefix:  fmt.Sprintf("could not fetch Wasm OCI image: could not fetch manifest: Get \"https://%s/v2/\"", ou.Host),
			wantVisitServer:     false,
		},
		{
			name:                 "fetch oci with wrong digest",
			initialCachedModules: map[moduleKey]cacheEntry{},
			fetchURL:             ociURLWithTag,
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				RequestTimeout:  time.Second * 10,
				Checksum:        "wrongdigest",
			},
			wantCachedModules:   map[moduleKey]*cacheEntry{},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantErrorMsgPrefix: fmt.Sprintf(
				"module downloaded from %v has checksum %v, which does not match:", fmt.Sprintf("oci://%s/test/valid/docker:v0.1.0", ou.Host), dockerImageDigest,
			),
			wantVisitServer: true,
		},
		{
			name:                 "fetch invalid oci",
			initialCachedModules: map[moduleKey]cacheEntry{},
			fetchURL:             fmt.Sprintf("oci://%s/test/invalid", ou.Host),
			getOptions: GetOptions{
				ResourceName:    "namespace.resource",
				ResourceVersion: "0",
				Checksum:        invalidOCIImageDigest,
				RequestTimeout:  time.Second * 10,
			},
			wantCachedModules:   map[moduleKey]*cacheEntry{},
			wantCachedChecksums: map[string]*checksumEntry{},
			wantErrorMsgPrefix: `could not fetch Wasm binary: the given image is in invalid format as an OCI image: 2 errors occurred:
	* could not parse as compat variant: invalid media type application/vnd.oci.image.layer.v1.tar (expect application/vnd.oci.image.layer.v1.tar+gzip)
	* could not parse as oci variant: number of layers must be 2 but got 1`,
			wantVisitServer: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			options := defaultOptions()
			if c.purgeInterval != 0 {
				options.PurgeInterval = c.purgeInterval
			}
			if c.wasmModuleExpiry != 0 {
				options.ModuleExpiry = c.wasmModuleExpiry
			}
			cache := NewLocalFileCache(tmpDir, options)
			cache.httpFetcher.initialBackoff = time.Microsecond
			defer close(cache.stopChan)

			var cacheHitKey *moduleKey
			initTime := time.Now()
			cache.mux.Lock()
			for k, m := range c.initialCachedModules {
				filePath := generateModulePath(t, tmpDir, k.name, m.modulePath)
				err := os.WriteFile(filePath, []byte("data/\n"), 0o644)
				if err != nil {
					t.Fatalf("failed to write initial wasm module file %v", err)
				}
				mkey := moduleKey{name: k.name, checksum: k.checksum}

				cache.modules[mkey] = &cacheEntry{modulePath: filePath, last: initTime}
				if m.referencingURLs != nil {
					cache.modules[mkey].referencingURLs = m.referencingURLs.Copy()
				} else {
					cache.modules[mkey].referencingURLs = sets.New[string]()
				}

				if moduleNameFromURL(c.fetchURL) == k.name && c.getOptions.Checksum == k.checksum {
					cacheHitKey = &mkey
				}
			}

			for k, m := range c.initialCachedChecksums {
				cache.checksums[k] = m
			}

			// put the tmp dir into the module path.
			for k, m := range c.wantCachedModules {
				c.wantCachedModules[k].modulePath = generateModulePath(t, tmpDir, k.name, m.modulePath)
			}
			cache.mux.Unlock()

			atomic.StoreInt32(&tsNumRequest, 0)
			if c.getOptions.PullSecret == nil {
				c.getOptions.PullSecret = []byte{}
			}
			gotFilePath, gotErr := cache.Get(c.fetchURL, c.getOptions)
			serverVisited := atomic.LoadInt32(&tsNumRequest) > 0

			if c.checkPurgeTimeout > 0 {
				moduleDeleted := false
				for start := time.Now(); time.Since(start) < c.checkPurgeTimeout; {
					fileCount := 0
					err = filepath.Walk(tmpDir,
						func(path string, info os.FileInfo, err error) error {
							if err != nil {
								return err
							}
							if !info.IsDir() {
								fileCount++
							}
							return nil
						})
					// Check existence of module files. files should be deleted before timing out.
					if err == nil && fileCount == 0 {
						moduleDeleted = true
						break
					}
				}

				if !moduleDeleted {
					t.Fatal("Wasm modules are not purged before purge timeout")
				}
			}

			cache.mux.Lock()
			if cacheHitKey != nil {
				if entry, ok := cache.modules[*cacheHitKey]; ok && entry.last == initTime {
					t.Errorf("Wasm module cache entry's last access time not updated after get operation, key: %v", *cacheHitKey)
				}
			}

			if diff := cmp.Diff(c.wantCachedModules, cache.modules,
				cmpopts.IgnoreFields(cacheEntry{}, "last", "referencingURLs"),
				cmp.AllowUnexported(cacheEntry{}),
			); diff != "" {
				t.Errorf("unexpected module cache: (-want, +got)\n%v", diff)
			}

			if diff := cmp.Diff(c.wantCachedChecksums, cache.checksums,
				cmp.AllowUnexported(checksumEntry{}),
			); diff != "" {
				t.Errorf("unexpected checksums: (-want, +got)\n%v", diff)
			}

			cache.mux.Unlock()

			wantFilePath := generateModulePath(t, tmpDir, moduleNameFromURL(c.fetchURL), c.wantFileName)
			if c.wantErrorMsgPrefix != "" {
				if gotErr == nil {
					t.Errorf("Wasm module cache lookup got no error, want error prefix `%v`", c.wantErrorMsgPrefix)
				} else if !strings.Contains(gotErr.Error(), c.wantErrorMsgPrefix) {
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

func setupOCIRegistry(t *testing.T, host string) (dockerImageDigest, invalidOCIImageDigest string) {
	// Push *compat* variant docker image (others are well tested in imagefetcher's test and the behavior is consistent).
	ref := fmt.Sprintf("%s/test/valid/docker:v0.1.0", host)
	binary := append(wasmHeader, []byte("this is wasm plugin")...)
	transport := remote.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // nolint: gosec // test only code
	fetchOpt := crane.WithTransport(transport)

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
	err = crane.Push(img, ref, fetchOpt)
	if err != nil {
		t.Fatal(err)
	}

	// Push image to the registry with latest tag as well
	ref = fmt.Sprintf("%s/test/valid/docker:latest", host)
	err = crane.Push(img, ref, fetchOpt)
	if err != nil {
		t.Fatal(err)
	}

	// Calculate sum
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
	err = crane.Push(img2, ref, fetchOpt)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestWasmCachePolicyChangesUsingHTTP(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewLocalFileCache(tmpDir, defaultOptions())
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
	url1 := ts.URL
	url2 := ts.URL + "/next"
	wantFilePath1 := generateModulePath(t, tmpDir, url1, fmt.Sprintf("%x.wasm", sha256.Sum256(binary1)))
	wantFilePath2 := generateModulePath(t, tmpDir, url2, fmt.Sprintf("%x.wasm", sha256.Sum256(binary2)))
	var defaultPullPolicy PullPolicy

	testWasmGet := func(downloadURL string, policy PullPolicy, resourceVersion string, wantFilePath string, wantNumRequest int) {
		t.Helper()
		gotFilePath, err := cache.Get(downloadURL, GetOptions{
			ResourceName:    "namespace.resource",
			ResourceVersion: resourceVersion,
			RequestTimeout:  time.Second * 10,
			PullSecret:      []byte{},
			PullPolicy:      policy,
		})
		if err != nil {
			t.Fatalf("failed to download Wasm module: %v", err)
		}
		if gotFilePath != wantFilePath {
			t.Fatalf("wasm download path got %v want %v", gotFilePath, wantFilePath)
		}
		if gotNumRequest != wantNumRequest {
			t.Fatalf("wasm download call got %v want %v", gotNumRequest, wantNumRequest)
		}
	}

	// 1st time: Initially load the binary1.
	testWasmGet(url1, defaultPullPolicy, "1", wantFilePath1, 1)
	// 2nd time: Should not pull the binary and use the cache because defaultPullPolicy is IfNotPresent
	testWasmGet(url1, defaultPullPolicy, "2", wantFilePath1, 1)
	// 3rd time: Should not pull the binary because the policy is IfNotPresent
	testWasmGet(url1, IfNotPresent, "3", wantFilePath1, 1)
	// 4th time: Should not pull the binary because the resource version is not changed
	testWasmGet(url1, Always, "3", wantFilePath1, 1)
	// 5th time: Should pull the binary because the resource version is changed.
	testWasmGet(url1, Always, "4", wantFilePath1, 2)
	// 6th time: Should pull the binary because URL is changed.
	testWasmGet(url2, Always, "4", wantFilePath2, 3)
}

func TestAllInsecureServer(t *testing.T) {
	tmpDir := t.TempDir()
	options := defaultOptions()
	options.InsecureRegistries = sets.New("*")
	cache := NewLocalFileCache(tmpDir, options)
	defer close(cache.stopChan)

	// Set up a fake registry for OCI images with TLS Server
	// Without "insecure" option, this should cause an error.
	tos := httptest.NewTLSServer(registry.New())
	defer tos.Close()
	ou, err := url.Parse(tos.URL)
	if err != nil {
		t.Fatal(err)
	}

	dockerImageDigest, _ := setupOCIRegistry(t, ou.Host)
	ociURLWithTag := fmt.Sprintf("oci://%s/test/valid/docker:v0.1.0", ou.Host)
	var defaultPullPolicy PullPolicy

	gotFilePath, err := cache.Get(ociURLWithTag, GetOptions{
		ResourceName:    "namespace.resource",
		ResourceVersion: "123456",
		RequestTimeout:  time.Second * 10,
		PullSecret:      []byte{},
		PullPolicy:      defaultPullPolicy,
	})
	if err != nil {
		t.Fatalf("failed to download Wasm module: %v", err)
	}

	wantFilePath := generateModulePath(t, tmpDir, moduleNameFromURL(ociURLWithTag), fmt.Sprintf("%s.wasm", dockerImageDigest))
	if gotFilePath != wantFilePath {
		t.Errorf("Wasm module local file path got %v, want %v", gotFilePath, wantFilePath)
	}
}

func generateModulePath(t *testing.T, baseDir, resourceName, filename string) string {
	t.Helper()
	sha := sha256.Sum256([]byte(resourceName))
	moduleDir := filepath.Join(baseDir, hex.EncodeToString(sha[:]))
	if _, err := os.Stat(moduleDir); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(moduleDir, 0o755)
		if err != nil {
			t.Fatalf("failed to create module dir %s: %v", moduleDir, err)
		}
	}
	return filepath.Join(moduleDir, filename)
}
