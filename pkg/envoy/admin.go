//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package envoy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"istio.io/istio/pkg/log"
)

// DrainListeners drains inbound listeners of Envoy so that inflight requests
// can gracefully finish and even continue making outbound calls as needed.
func DrainListeners(adminPort uint32, inboundonly bool, skipExit bool) error {
	var drainURL string
	if inboundonly {
		drainURL = "drain_listeners?inboundonly&graceful"
	} else {
		drainURL = "drain_listeners?graceful"
	}
	if skipExit {
		drainURL += "&skip_exit"
	}
	res, err := doEnvoyPost(drainURL, "", "", adminPort)
	log.Debugf("Drain listener endpoint response : %s", res.String())
	return err
}

func doEnvoyPost(path, contentType, body string, adminPort uint32) (*bytes.Buffer, error) {
	requestURL := fmt.Sprintf("http://localhost:%d/%s", adminPort, path)
	buffer, err := doHTTPPost(requestURL, contentType, body)
	if err != nil {
		return nil, err
	}
	return buffer, nil
}

func doHTTPPost(requestURL, contentType, body string) (*bytes.Buffer, error) {
	response, err := http.Post(requestURL, contentType, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	var b bytes.Buffer
	if _, err := io.Copy(&b, response.Body); err != nil {
		return nil, err
	}
	return &b, nil
}
