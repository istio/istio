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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestWasmHTTPFetch(t *testing.T) {
	var ts *httptest.Server

	cases := []struct {
		name           string
		handler        func(http.ResponseWriter, *http.Request, int)
		timeout        time.Duration
		wantNumRequest int
		wantErrorRegex string
	}{
		{
			name: "download ok",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				fmt.Fprintln(w, "wasm")
			},
			timeout:        5 * time.Second,
			wantNumRequest: 1,
		},
		{
			name: "download retry",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				if num <= 2 {
					w.WriteHeader(500)
				} else {
					fmt.Fprintln(w, "wasm")
				}
			},
			timeout:        5 * time.Second,
			wantNumRequest: 4,
		},
		{
			name: "download max retry",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				w.WriteHeader(500)
			},
			timeout:        5 * time.Second,
			wantNumRequest: 5,
			wantErrorRegex: "wasm module download failed after 5 attempts, last error: wasm module download request failed: status code 500",
		},
		{
			name: "download is never tried by immediate context timeout",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				w.WriteHeader(500)
			},
			timeout:        0, // Immediately timeout in the context level.
			wantNumRequest: 0, // Should not retried because it is already timed out.
			wantErrorRegex: "wasm module download failed after 1 attempts, last error: Get \"[^\"]+\": context deadline exceeded",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotNumRequest := 0
			wantWasmModule := "wasm\n"
			ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.handler(w, r, gotNumRequest)
				gotNumRequest++
			}))
			defer ts.Close()
			fetcher := NewHTTPFetcher(DefaultHTTPRequestTimeout, DefaultHTTPRequestMaxRetries)
			fetcher.initialBackoff = time.Microsecond
			ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
			defer cancel()
			b, err := fetcher.Fetch(ctx, ts.URL, false)
			if c.wantNumRequest != gotNumRequest {
				t.Errorf("Wasm download request got %v, want %v", gotNumRequest, c.wantNumRequest)
			}
			if c.wantErrorRegex != "" {
				if err == nil {
					t.Errorf("Wasm download got no error, want error regex `%v`", c.wantErrorRegex)
				} else if matched, regexErr := regexp.MatchString(c.wantErrorRegex, err.Error()); regexErr != nil || !matched {
					t.Errorf("Wasm download got error `%v`, want error regex `%v`", err, c.wantErrorRegex)
				}
			} else if string(b) != wantWasmModule {
				t.Errorf("downloaded wasm module got %v, want wasm", string(b))
			}
		})
	}
}

func TestWasmHTTPInsecureServer(t *testing.T) {
	var ts *httptest.Server

	cases := []struct {
		name            string
		handler         func(http.ResponseWriter, *http.Request, int)
		insecure        bool
		wantNumRequest  int
		wantErrorSuffix string
	}{
		{
			name: "download fail",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				fmt.Fprintln(w, "wasm")
			},
			insecure:        false,
			wantErrorSuffix: "x509: certificate signed by unknown authority",
			wantNumRequest:  0,
		},
		{
			name: "download ok",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				fmt.Fprintln(w, "wasm")
			},
			insecure:       true,
			wantNumRequest: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotNumRequest := 0
			wantWasmModule := "wasm\n"
			ts = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.handler(w, r, gotNumRequest)
				gotNumRequest++
			}))
			defer ts.Close()
			fetcher := NewHTTPFetcher(DefaultHTTPRequestTimeout, DefaultHTTPRequestMaxRetries)
			fetcher.initialBackoff = time.Microsecond
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			b, err := fetcher.Fetch(ctx, ts.URL, c.insecure)
			if c.wantNumRequest != gotNumRequest {
				t.Errorf("Wasm download request got %v, want %v", gotNumRequest, c.wantNumRequest)
			}
			if c.wantErrorSuffix != "" {
				if err == nil {
					t.Errorf("Wasm download got no error, want error suffix `%v`", c.wantErrorSuffix)
				} else if !strings.HasSuffix(err.Error(), c.wantErrorSuffix) {
					t.Errorf("Wasm download got error `%v`, want error suffix `%v`", err, c.wantErrorSuffix)
				}
			} else if string(b) != wantWasmModule {
				t.Errorf("downloaded wasm module got %v, want wasm", string(b))
			}
		})
	}
}

func createTar(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{
		Name: "plugin.wasm",
		Mode: 0o600,
		Size: int64(len(b)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func createGZ(t *testing.T, b []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func TestWasmHTTPFetchCompressedOrTarFile(t *testing.T) {
	wasmBinary := append(wasmMagicNumber, 0x00, 0x00, 0x00, 0x00)
	tarball := createTar(t, wasmBinary)
	gz := createGZ(t, wasmBinary)
	gzTarball := createGZ(t, tarball)
	cases := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request, int)
	}{
		{
			name: "plain wasm binary",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				w.Write(wasmBinary)
			},
		},
		{
			name: "tarball of wasm binary",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				w.Write(tarball)
			},
		},
		{
			name: "gzipped wasm binary",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				w.Write(gz)
			},
		},
		{
			name: "gzipped tarball of wasm binary",
			handler: func(w http.ResponseWriter, r *http.Request, num int) {
				w.Write(gzTarball)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotNumRequest := 0
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.handler(w, r, gotNumRequest)
				gotNumRequest++
			}))
			defer ts.Close()
			fetcher := NewHTTPFetcher(DefaultHTTPRequestTimeout, DefaultHTTPRequestMaxRetries)
			fetcher.initialBackoff = time.Microsecond
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			b, err := fetcher.Fetch(ctx, ts.URL, false)
			if err != nil {
				t.Errorf("Wasm download got an unexpected error: %v", err)
			}

			if diff := cmp.Diff(wasmBinary, b); diff != "" {
				if len(diff) > 500 {
					diff = diff[:500]
				}
				t.Errorf("unexpected binary: (-want, +got)\n%v", diff)
			}
		})
	}
}
