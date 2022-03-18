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
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// HTTPFetcher fetches remote wasm module with HTTP get.
type HTTPFetcher struct {
	defaultClient  *http.Client
	initialBackoff time.Duration
}

// NewHTTPFetcher create a new HTTP remote wasm module fetcher.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{
		defaultClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		initialBackoff: time.Millisecond * 500,
	}
}

// Fetch downloads a wasm module with HTTP get.
func (f *HTTPFetcher) Fetch(url string, timeout time.Duration) ([]byte, error) {
	c := f.defaultClient
	if timeout != 0 {
		c = &http.Client{
			Timeout: timeout,
		}
	}
	attempts := 0

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = f.initialBackoff
	b.Reset()
	var lastError error
	for attempts < 5 {
		attempts++
		resp, err := c.Get(url)
		if err != nil {
			lastError = err
			wasmLog.Debugf("wasm module download request failed: %v", err)
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
	return nil, fmt.Errorf("wasm module download failed, last error: %v", lastError)
}

func retryable(code int) bool {
	return code >= 500 &&
		!(code == http.StatusNotImplemented ||
			code == http.StatusHTTPVersionNotSupported ||
			code == http.StatusNetworkAuthenticationRequired)
}
