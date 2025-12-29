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
	return request("GET", requestURL, t, nil)
}

func DoHTTPGet(requestURL string) (*bytes.Buffer, error) {
	return DoHTTPGetWithTimeout(requestURL, requestTimeout)
}

func GET(requestURL string, t time.Duration, headers map[string]string) (*bytes.Buffer, error) {
	return request("GET", requestURL, t, headers)
}

func PUT(requestURL string, t time.Duration, headers map[string]string) (*bytes.Buffer, error) {
	return request("PUT", requestURL, t, headers)
}

func request(method, requestURL string, t time.Duration, headers map[string]string) (*bytes.Buffer, error) {
	httpClient := &http.Client{
		Timeout: t,
	}

	req, err := http.NewRequest(method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	response, err := httpClient.Do(req)
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
