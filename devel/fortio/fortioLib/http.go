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
	"net/http/httputil"
)

// Verbose controls verbose/debug output, higher more verbose.
var Verbose int

// Version is the fortio package version (TODO:auto gen/extract).
var Version = "0.1"
var userAgent = "istio/fortio-" + Version

// newHttpRequest makes a new http GET request for url with User-Agent
func newHTTPRequest(url string) *http.Request {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("unable to make request for %s : %v", url, err)
	}
	req.Header.Add("User-Agent", userAgent)
	if Verbose > 2 {
		bytes, err := httputil.DumpRequestOut(req, false)
		if err != nil {
			log.Printf("unable to dump request %v", err)
		} else {
			log.Printf("For URL %s, sending:\n%s", url, bytes)
		}
	}
	return req
}

// FetchURL fetches URL contenty and does error handling/logging
func FetchURL(url string) (int, []byte) {
	req := newHTTPRequest(url)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("unable to send request for %s : %v", url, err)
		return http.StatusBadRequest, []byte(err.Error())
	}
	defer resp.Body.Close() //nolint(errcheck)
	if Verbose > 2 {
		bytes, e := httputil.DumpResponse(resp, false)
		if e != nil {
			log.Printf("unable to dump response %v", e)
		} else {
			log.Printf("For URL %s, received:\n%s", url, bytes)
		}
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
	if Verbose > 1 {
		log.Printf("Got %d : %s for %s - response is %d bytes", code, resp.Status, url, len(body))
	}
	return code, body
}
