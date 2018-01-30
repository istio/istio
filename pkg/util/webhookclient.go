// Copyright 2018 Istio Authors
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

package util

import (
	"context"
	"net"
	"net/http"
	"strings"
	"time"
)

// NewWebHookClient takes URLs of the form http://foo.com,
// unix+http://foo.com, and returns a base URL and http client that can be
// used to communicate with the endpoint over IP or unix domain socket.
func NewWebHookClient(apiEndpoint string) (string, *http.Client) {
	if len(apiEndpoint) == 0 {
		return "", nil
	}

	transport := &http.Transport{}
	strippedEndpoint := apiEndpoint

	if strings.Contains(apiEndpoint, "unix://") {
		transport.DialContext = func(_ context.Context, _, addr string) (net.Conn, error) {
			return net.Dial("unix", addr)
		}

		// strip the +unix and convert to plain http://
		if strings.Index(apiEndpoint, "unix://") == 0 {
			strippedEndpoint = strings.Replace(apiEndpoint, "unix", "", 1)
		} else {
			strippedEndpoint = strings.Replace(apiEndpoint, "+unix", "", 1)
		}
	}

	return strippedEndpoint, &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}
}
