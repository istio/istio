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

package echo

import (
	"fmt"
	"sort"
	"strings"
)

// Response represents a response to a single echo request.
type Response struct {
	// RequestURL is the requested URL. This differs from URL, which is the just the path.
	// For example, RequestURL=http://foo/bar, URL=/bar
	RequestURL string
	// Body is the body of the response
	Body string
	// ID is a unique identifier of the resource in the response
	ID string
	// URL is the url the request is sent to
	URL string
	// Version is the version of the resource in the response
	Version string
	// Port is the port of the resource in the response
	Port string
	// Code is the response code
	Code string
	// Host is the host called by the request
	Host string
	// Hostname is the host that responded to the request
	Hostname string
	// The cluster where the server is deployed.
	Cluster string
	// IstioVersion for the Istio sidecar.
	IstioVersion string
	// IP is the requester's ip address
	IP string
	// RawResponse gives a map of all values returned in the response (headers, etc)
	RawResponse map[string]string
}

// Count occurrences of the given text within the body of this response.
func (r Response) Count(text string) int {
	return strings.Count(r.Body, text)
}

// ResponseBody returns the body of the response, in order
func (r Response) ResponseBody() []string {
	type kv struct {
		k, v string
	}
	kvs := []kv{}
	// RawResponse is in random order, so get the order back via sorting.
	for k, v := range r.RawResponse {
		kvs = append(kvs, kv{k, v})
	}
	sort.Slice(kvs, func(i, j int) bool {
		return kvs[i].k < kvs[j].k
	})
	resp := []string{}
	for _, v := range kvs {
		resp = append(resp, v.v)
	}
	return resp
}

func (r Response) String() string {
	out := ""
	out += fmt.Sprintf("Body:         %s\n", r.Body)
	out += fmt.Sprintf("ID:           %s\n", r.ID)
	out += fmt.Sprintf("URL:          %s\n", r.URL)
	out += fmt.Sprintf("Version:      %s\n", r.Version)
	out += fmt.Sprintf("Port:         %s\n", r.Port)
	out += fmt.Sprintf("Code:         %s\n", r.Code)
	out += fmt.Sprintf("Host:         %s\n", r.Host)
	out += fmt.Sprintf("Hostname:     %s\n", r.Hostname)
	out += fmt.Sprintf("Cluster:      %s\n", r.Cluster)
	out += fmt.Sprintf("IstioVersion: %s\n", r.IstioVersion)
	out += fmt.Sprintf("IP:           %s\n", r.IP)

	return out
}
