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
	"io/ioutil"
	"net/http"
	"time"
)

const (
	defaultRetryBackoff = 50 * time.Millisecond
)

// HTTPFetcher fetches remote wasm module with HTTP get.
type HTTPFetcher struct {
	defaultClient *http.Client
	// TODO(bianpengyuan): make this exponential backoff.
	retryBackoff time.Duration
}

// NewHTTPFetcher create a new HTTP remote wasm module fetcher.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{
		defaultClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		retryBackoff: defaultRetryBackoff,
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
	var lastError error
	for attempts < 5 {
		attempts++
		resp, err := c.Get(url)
		if err != nil {
			lastError = err
			wasmLog.Debugf("wasm module download request failed: %v", err)
			time.Sleep(f.retryBackoff)
			continue
		}
		if resp.StatusCode == http.StatusOK {
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			return body, err
		}
		lastError = fmt.Errorf("wasm module download request failed: status code %v", resp.StatusCode)
		if retryable(resp.StatusCode) {
			body, _ := ioutil.ReadAll(resp.Body)
			wasmLog.Debugf("wasm module download failed: status code %v, body %v", resp.StatusCode, string(body))
			resp.Body.Close()
			time.Sleep(f.retryBackoff)
			continue
		}
	}
	return nil, fmt.Errorf("wasm module download failed, last error: %v", lastError)
}

func retryable(code int) bool {
	return code >= 500 && !(code == 501 || code == 505 || code == 511)
}
