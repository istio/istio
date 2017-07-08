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
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"strings"
)

// ExtraHeaders to be added to each request.
var extraHeaders http.Header

// Host is treated specially, remember that one separately.
var hostOverride string

func init() {
	extraHeaders = make(http.Header)
	extraHeaders.Add("User-Agent", userAgent)
}

// Verbosity controls verbose/debug output level, higher more verbose.
var Verbosity int

// Version is the fortio package version (TODO:auto gen/extract).
const (
	Version   = "0.1"
	userAgent = "istio/fortio-" + Version
)

// AddAndValidateExtraHeader collects extra headers (see main.go for example).
func AddAndValidateExtraHeader(h string) error {
	s := strings.SplitN(h, ":", 2)
	if len(s) != 2 {
		return fmt.Errorf("invalid extra header '%s', expecting Key: Value", h)
	}
	key := strings.TrimSpace(s[0])
	value := strings.TrimSpace(s[1])
	// Not checking Verbosity as this is called during flag parsing and Verbosity isn't set yet
	if strings.EqualFold(key, "host") {
		log.Printf("Will be setting special Host header to %s", value)
		hostOverride = value
	} else {
		log.Printf("Setting regular extra header %s: %s", key, value)
		extraHeaders.Add(key, value)
	}
	return nil
}

// newHttpRequest makes a new http GET request for url with User-Agent.
func newHTTPRequest(url string) *http.Request {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Unable to make request for %s : %v", url, err)
		return nil
	}
	req.Header = extraHeaders
	if hostOverride != "" {
		req.Host = hostOverride
	}
	if Verbosity < 3 {
		return req
	}
	bytes, err := httputil.DumpRequestOut(req, false)
	if err != nil {
		log.Printf("Unable to dump request %v", err)
	} else {
		log.Printf("For URL %s, sending:\n%s", url, bytes)
	}
	return req
}

// FetchURL fetches URL contenty and does error handling/logging.
func FetchURL(url string) (int, []byte) {
	req := newHTTPRequest(url)
	if req == nil {
		return http.StatusBadRequest, []byte("bad url")
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Unable to send request for %s : %v", url, err)
		return http.StatusBadRequest, []byte(err.Error())
	}
	var data []byte
	if Verbosity > 2 {
		if data, err = httputil.DumpResponse(resp, false); err != nil {
			log.Printf("Unable to dump response %v", err)
		} else {
			log.Printf("For URL %s, received:\n%s", url, data)
		}
	}
	data, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close() //nolint(errcheck)
	if err != nil {
		log.Printf("Unable to read response for %s : %v", url, err)
		code := resp.StatusCode
		if code == http.StatusOK {
			code = http.StatusNoContent
			log.Printf("Ok code despite read error, switching code to %d", code)
		}
		return code, data
	}
	code := resp.StatusCode
	if Verbosity > 1 {
		log.Printf("Got %d : %s for %s - response is %d bytes", code, resp.Status, url, len(data))
	}
	return code, data
}
