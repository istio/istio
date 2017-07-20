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
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"
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

// Client object for making repeated requests of the same URL using the same
// http client
type Client struct {
	url    string
	req    *http.Request
	client *http.Client
}

// FetchURL fetches URL contenty and does error handling/logging.
func FetchURL(url string) (int, []byte) {
	client := NewClient(url, 1, true)
	if client == nil {
		return http.StatusBadRequest, []byte("bad url")
	}
	return client.Fetch()
}

// Fetch fetches the byte and code for pre created client
func (c *Client) Fetch() (int, []byte) {
	resp, err := c.client.Do(c.req)
	if err != nil {
		log.Printf("Unable to send request for %s : %v", c.url, err)
		return http.StatusBadRequest, []byte(err.Error())
	}
	var data []byte
	if Verbosity > 2 {
		if data, err = httputil.DumpResponse(resp, false); err != nil {
			log.Printf("Unable to dump response %v", err)
		} else {
			log.Printf("For URL %s, received:\n%s", c.url, data)
		}
	}
	data, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close() //nolint(errcheck)
	if err != nil {
		log.Printf("Unable to read response for %s : %v", c.url, err)
		code := resp.StatusCode
		if code == http.StatusOK {
			code = http.StatusNoContent
			log.Printf("Ok code despite read error, switching code to %d", code)
		}
		return code, data
	}
	code := resp.StatusCode
	if Verbosity > 1 {
		log.Printf("Got %d : %s for %s - response is %d bytes", code, resp.Status, c.url, len(data))
	}
	return code, data
}

// NewClient creates a client object
func NewClient(url string, numConnections int, compression bool) *Client {
	req := newHTTPRequest(url)
	if req == nil {
		return nil
	}
	client := Client{
		url,
		req,
		&http.Client{
			Timeout: 3 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        numConnections,
				MaxIdleConnsPerHost: numConnections,
				DisableCompression:  !compression,
				Dial: (&net.Dialer{
					Timeout: 1 * time.Second,
				}).Dial,
				TLSHandshakeTimeout: 1 * time.Second,
			},
		},
	}
	return &client
}
