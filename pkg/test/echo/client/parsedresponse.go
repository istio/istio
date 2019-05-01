//  Copyright 2019 Istio Authors
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

package client

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/echo/common/response"
	"istio.io/istio/pkg/test/echo/proto"
)

var (
	requestIDFieldRegex      = regexp.MustCompile("(?i)" + string(response.RequestIDField) + "=(.*)")
	serviceVersionFieldRegex = regexp.MustCompile(string(response.ServiceVersionField) + "=(.*)")
	servicePortFieldRegex    = regexp.MustCompile(string(response.ServicePortField) + "=(.*)")
	statusCodeFieldRegex     = regexp.MustCompile(string(response.StatusCodeField) + "=(.*)")
	hostFieldRegex           = regexp.MustCompile(string(response.HostField) + "=(.*)")
	hostnameFieldRegex       = regexp.MustCompile(string(response.HostnameField) + "=(.*)")
)

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
	// Host is the host called by the request
	Host string
	// Hostname is the host that responded to the request
	Hostname string
}

// IsOK indicates whether or not the code indicates a successful request.
func (r *ParsedResponse) IsOK() bool {
	return r.Code == response.StatusCodeOK
}

// Count occurrences of the given text within the body of this response.
func (r *ParsedResponse) Count(text string) int {
	return strings.Count(r.Body, text)
}

// ParsedResponses is an ordered list of parsed response objects.
type ParsedResponses []*ParsedResponse

// Len returns the length of the parsed responses.
func (r ParsedResponses) Len() int {
	return len(r)
}

func (r ParsedResponses) Check(check func(int, *ParsedResponse) error) (err error) {
	if r.Len() == 0 {
		return fmt.Errorf("no responses received")
	}

	for i, resp := range r {
		if e := check(i, resp); e != nil {
			err = multierror.Append(err, e)
		}
	}
	return
}

func (r ParsedResponses) CheckOrFail(t testing.TB, check func(int, *ParsedResponse) error) ParsedResponses {
	if err := r.Check(check); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r ParsedResponses) CheckOK() error {
	return r.Check(func(i int, response *ParsedResponse) error {
		if !response.IsOK() {
			return fmt.Errorf("response[%d] Status Code: %s", i, response.Code)
		}
		return nil
	})
}

func (r ParsedResponses) CheckOKOrFail(t testing.TB) ParsedResponses {
	if err := r.CheckOK(); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r ParsedResponses) CheckHost(expected string) error {
	return r.Check(func(i int, response *ParsedResponse) error {
		if response.Host != expected {
			return fmt.Errorf("response[%d] Host: expected %s, received %s", i, expected, response.Host)
		}
		return nil
	})
}

func (r ParsedResponses) CheckHostOrFail(t testing.TB, expected string) ParsedResponses {
	if err := r.CheckHost(expected); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r ParsedResponses) CheckPort(expected int) error {
	expectedStr := strconv.Itoa(expected)
	return r.Check(func(i int, response *ParsedResponse) error {
		if response.Port != expectedStr {
			return fmt.Errorf("response[%d] Port: expected %s, received %s", i, expectedStr, response.Port)
		}
		return nil
	})
}

func (r ParsedResponses) CheckPortOrFail(t testing.TB, expected int) ParsedResponses {
	if err := r.CheckPort(expected); err != nil {
		t.Fatal(err)
	}
	return r
}

// Count occurrences of the given text within the bodies of all responses.
func (r ParsedResponses) Count(text string) int {
	count := 0
	for _, c := range r {
		count += c.Count(text)
	}
	return count
}

func parseForwardedResponse(resp *proto.ForwardEchoResponse) ParsedResponses {
	responses := make([]*ParsedResponse, len(resp.Output))
	for i, output := range resp.Output {
		responses[i] = parseResponse(output)
	}
	return responses
}

func parseResponse(output string) *ParsedResponse {
	out := ParsedResponse{
		Body: output,
	}

	match := requestIDFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.ID = match[1]
	}

	match = serviceVersionFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Version = match[1]
	}

	match = servicePortFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Port = match[1]
	}

	match = statusCodeFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Code = match[1]
	}

	match = hostFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Host = match[1]
	}

	match = hostnameFieldRegex.FindStringSubmatch(output)
	if match != nil {
		out.Hostname = match[1]
	}

	return &out
}
