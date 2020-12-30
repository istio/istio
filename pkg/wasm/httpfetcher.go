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
)

type HTTPFetcher struct {
	client *http.Client
}

// TODO: add retry and timeout.
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{client: http.DefaultClient}
}

func (f *HTTPFetcher) Fetch(url string) ([]byte, error) {
	resp, err := f.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("http fetcher: fail to fetch wasm Module: %v", err)
	}

	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}
