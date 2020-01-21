// Copyright 2019 Istio Authors
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

package httprequest

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// Get sends an HTTP GET request and returns the result.
func Get(url string) ([]byte, error) {
	buf := bytes.NewBuffer(nil)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			DisableCompression: true,
			Proxy:              http.ProxyFromEnvironment,
		}}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch URL %s : %s", url, resp.Status)
	}

	_, err = io.Copy(buf, resp.Body)
	return buf.Bytes(), err
}
