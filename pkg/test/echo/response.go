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
	"net/http"
	"sort"
	"strings"
)

// HeaderType is a helper enum for retrieving Headers from a Response.
type HeaderType string

const (
	RequestHeader  HeaderType = "request"
	ResponseHeader HeaderType = "response"
)

// Response represents a response to a single echo request.
type Response struct {
	// RequestURL is the requested URL. This differs from URL, which is the just the path.
	// For example, RequestURL=http://foo/bar, URL=/bar
	RequestURL string
	// Method used (for HTTP).
	Method string
	// Protocol used for the request.
	Protocol string
	// Alpn value (for TLS).
	Alpn string
	// SNI value (for TLS).
	SNI string
	// ProxyProtocol value.
	ProxyProtocol string
	// RawContent is the original unparsed content for this response
	RawContent string
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
	// IP is the client IP, as seen by the server.
	IP string
	// SourceIP is the client's source IP. This can differ from IP when there are multiple hops.
	SourceIP string
	// RawBody gives a map of all key/values in the body of the response.
	RawBody         map[string]string
	RequestHeaders  http.Header
	ResponseHeaders http.Header
}

// Count occurrences of the given text within the body of this response.
func (r Response) Count(text string) int {
	return strings.Count(r.RawContent, text)
}

// GetHeaders returns the appropriate headers for the given type.
func (r Response) GetHeaders(hType HeaderType) http.Header {
	switch hType {
	case RequestHeader:
		return r.RequestHeaders
	case ResponseHeader:
		return r.ResponseHeaders
	default:
		panic("invalid HeaderType enum: " + hType)
	}
}

// Body returns the lines of the response body, in order
func (r Response) Body() []string {
	type keyValue struct {
		k, v string
	}
	var keyValues []keyValue
	// RawBody is in random order, so get the order back via sorting.
	for k, v := range r.RawBody {
		keyValues = append(keyValues, keyValue{k, v})
	}
	sort.Slice(keyValues, func(i, j int) bool {
		return keyValues[i].k < keyValues[j].k
	})
	var resp []string
	for _, kv := range keyValues {
		resp = append(resp, kv.v)
	}
	return resp
}

func (r Response) String() string {
	out := ""
	out += fmt.Sprintf("RawContent:       %s\n", r.RawContent)
	out += fmt.Sprintf("ID:               %s\n", r.ID)
	out += fmt.Sprintf("Method:           %s\n", r.Method)
	out += fmt.Sprintf("Protocol:         %s\n", r.Protocol)
	out += fmt.Sprintf("Alpn:             %s\n", r.Alpn)
	out += fmt.Sprintf("URL:              %s\n", r.URL)
	out += fmt.Sprintf("Version:          %s\n", r.Version)
	out += fmt.Sprintf("Port:             %s\n", r.Port)
	out += fmt.Sprintf("Code:             %s\n", r.Code)
	out += fmt.Sprintf("Host:             %s\n", r.Host)
	out += fmt.Sprintf("Hostname:         %s\n", r.Hostname)
	out += fmt.Sprintf("Cluster:          %s\n", r.Cluster)
	out += fmt.Sprintf("IstioVersion:     %s\n", r.IstioVersion)
	out += fmt.Sprintf("IP:               %s\n", r.IP)
	out += fmt.Sprintf("Request Headers:  %v\n", r.RequestHeaders)
	out += fmt.Sprintf("Response Headers: %v\n", r.ResponseHeaders)

	return out
}
