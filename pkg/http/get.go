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

package http

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"
)

const requestTimeout = time.Second * 1 // Default timeout.

func DoHTTPGetWithTimeout(requestURL string, t time.Duration) (*bytes.Buffer, error) {
	httpClient := &http.Client{
		Timeout: t,
	}

	response, err := httpClient.Get(requestURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", response.StatusCode)
	}

	var b bytes.Buffer
	if _, err := io.Copy(&b, response.Body); err != nil {
		return nil, err
	}
	return &b, nil
}

func DoHTTPGet(requestURL string) (*bytes.Buffer, error) {
	return DoHTTPGetWithTimeout(requestURL, requestTimeout)
}
