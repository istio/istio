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

package echo

import (
	"regexp"

	"istio.io/istio/pkg/test/service/echo/proto"
)

const (
	codeOK = "200"
)

var (
	idRegex      = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRegex = regexp.MustCompile("ServiceVersion=(.*)")
	portRegex    = regexp.MustCompile("ServicePort=(.*)")
	codeRegex    = regexp.MustCompile("StatusCode=(.*)")
	hostRegex    = regexp.MustCompile("Host=(.*)")
)

// ParseForwardedResponse parses each echo response in the forwarded response.
func ParseForwardedResponse(resp *proto.ForwardEchoResponse) []*ParsedResponse {
	responses := make([]*ParsedResponse, len(resp.Output))
	for i, output := range resp.Output {
		responses[i] = ParseResponse(output)
	}
	return responses
}

// ParseResponse parses a single echo response body
func ParseResponse(output string) *ParsedResponse {
	out := ParsedResponse{
		Body: output,
	}

	match := idRegex.FindStringSubmatch(output)
	if match != nil {
		out.ID = match[1]
	}

	match = versionRegex.FindStringSubmatch(output)
	if match != nil {
		out.Version = match[1]
	}

	match = portRegex.FindStringSubmatch(output)
	if match != nil {
		out.Port = match[1]
	}

	match = codeRegex.FindStringSubmatch(output)
	if match != nil {
		out.Code = match[1]
	}

	match = hostRegex.FindStringSubmatch(output)
	if match != nil {
		out.Host = match[1]
	}

	return &out
}

// ParsedResponse represents a response to a single echo request.
type ParsedResponse struct {
	// Body is the body of the response
	Body string
	// ID is a unique identifier of the resource in the response
	ID string
	// Version is the version of the resource in the response
	Version string
	// Port is the port of the resource in the response
	Port string
	// Code is the response code
	Code string
	// Host is the host returned by the response
	Host string
}

// IsOK indicates whether or not the code indicates a successful request.
func (r *ParsedResponse) IsOK() bool {
	return r.Code == codeOK
}
