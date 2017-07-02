// Copyright 2017 Istio Authors
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

package fortio

import (
	"io/ioutil"
	"log"
	"net/http"
)

// newHttpRequest makes a new http GET request for url with User-Agent
func newHttpRequest(url string) *http.Request {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("unable to make request for %s : %v", url, err)
	}
	req.Header.Add("User-Agent", "istio/fortio-0.1")
	return req
}

// FetchURL fetches URL contenty and does error handling/logging
func FetchURL(url string) (int, []byte) {
	req := newHttpRequest(url)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("unable to send request for %s : %v", url, err)
		return http.StatusBadRequest, []byte(err.Error())
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("unable to read response for %s : %v", url, err)
		code := resp.StatusCode
		if code == http.StatusOK {
			code = http.StatusNoContent
			log.Printf("Ok code despite read error, switching code to %d", code)
		}
		return code, body
	}
	code := resp.StatusCode
	log.Printf("Got %d : %s for %s - response is %d bytes", code, resp.Status, url, len(body))
	return code, body
}
