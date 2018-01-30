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
)

func NewWebHookClient(url string) *http.Client {
	if len(url) == 0 {
		return nil
	}

	transport := &http.Transport{}

	if strings.Contains(url, "unix://") {
		transport.DialContext = func(_ context.Context, _, addr string) (net.Conn, error) {
			return net.Dial("unix", addr)
		}

		// strip the +unix and convert to plain http://
		if strings.Index(o.WebhookEndpoint, "unix://") == 0 {
			out.webhookEndpoint = strings.Replace(o.WebhookEndpoint, "unix", "", 1)
		} else {
			out.webhookEndpoint = strings.Replace(o.WebhookEndpoint, "+unix", "", 1)
		}
	}

	return &http.Client{Transport: transport}
}
