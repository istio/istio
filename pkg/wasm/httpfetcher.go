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
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// HTTPFetcher fetches remote wasm module with HTTP get.
type HTTPFetcher struct {
	client         *http.Client
	insecureClient *http.Client
	initialBackoff time.Duration
}

// NewHTTPFetcher create a new HTTP remote wasm module fetcher.
// requestTimeout is a timeout for each HTTP/HTTPS request.
func NewHTTPFetcher(requestTimeout time.Duration) *HTTPFetcher {
	if requestTimeout == 0 {
		requestTimeout = 5 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	return &HTTPFetcher{
		client: &http.Client{
			Timeout: requestTimeout,
		},
		insecureClient: &http.Client{
			Timeout:   requestTimeout,
			Transport: transport,
		},
		initialBackoff: time.Millisecond * 500,
	}
}

// Fetch downloads a wasm module with HTTP get.
func (f *HTTPFetcher) Fetch(ctx context.Context, url string, allowInsecure bool) ([]byte, error) {
	c := f.client
	if allowInsecure {
		c = f.insecureClient
	}
	attempts := 0
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = f.initialBackoff
	b.Reset()
	var lastError error
	for attempts < DefaultWasmHTTPRequestMaxRetries {
		attempts++
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			wasmLog.Debugf("wasm module download request failed: %v", err)
			return nil, err
		}
		resp, err := c.Do(req)
		if err != nil {
			lastError = err
			wasmLog.Debugf("wasm module download request failed: %v", err)
			if ctx.Err() != nil {
				// If there is context timeout, exit this loop.
				return nil, fmt.Errorf("wasm module download failed after %v attempts, last error: %v", attempts, lastError)
			}
			time.Sleep(b.NextBackOff())
			continue
		}
		if resp.StatusCode == http.StatusOK {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			return body, err
		}
		lastError = fmt.Errorf("wasm module download request failed: status code %v", resp.StatusCode)
		if retryable(resp.StatusCode) {
			body, _ := io.ReadAll(resp.Body)
			wasmLog.Debugf("wasm module download failed: status code %v, body %v", resp.StatusCode, string(body))
			resp.Body.Close()
			time.Sleep(b.NextBackOff())
			continue
		}
		resp.Body.Close()
		break
	}
	return nil, fmt.Errorf("wasm module download failed after %v attempts, last error: %v", attempts, lastError)
}

func retryable(code int) bool {
	return code >= 500 &&
		!(code == http.StatusNotImplemented ||
			code == http.StatusHTTPVersionNotSupported ||
			code == http.StatusNetworkAuthenticationRequired)
}
