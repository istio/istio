//  Copyright 2018 Istio Authors
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
	"context"
	"regexp"
	"strings"

	"google.golang.org/grpc"

	"istio.io/istio/pkg/test/application/echo/proto"
)

const (
	codeOK = "200"
)

var (
	idRegex       = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRegex  = regexp.MustCompile("ServiceVersion=(.*)")
	portRegex     = regexp.MustCompile("ServicePort=(.*)")
	codeRegex     = regexp.MustCompile("StatusCode=(.*)")
	hostRegex     = regexp.MustCompile("Host=(.*)")
	hostnameRegex = regexp.MustCompile("Hostname=(.*)")
)

// Client is a simple client for forwarding echo requests between echo applications.
type Client struct {
	conn   *grpc.ClientConn
	client proto.EchoTestServiceClient
}

// NewClient creates a new EchoClient instance that is connected to the given address.
func NewClient(address string) (*Client, error) {
	// Connect to the GRPC (command) endpoint of 'this' app.
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}
	client := proto.NewEchoTestServiceClient(conn)

	return &Client{
		conn:   conn,
		client: client,
	}, nil
}

// Close the EchoClient and free any resources.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ForwardEcho sends the given forward request and parses the response for easier processing. Only fails if the request fails.
func (c *Client) ForwardEcho(request *proto.ForwardEchoRequest) (ParsedResponses, error) {
	// Forward a request from 'this' service to the destination service.
	resp, err := c.client.ForwardEcho(context.Background(), request)
	if err != nil {
		return nil, err
	}

	return parseForwardedResponse(resp), nil
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
	// Host is the host called by the request
	Host string
	// Hostname is the host that responded to the request
	Hostname string
}

// IsOK indicates whether or not the code indicates a successful request.
func (r *ParsedResponse) IsOK() bool {
	return r.Code == codeOK
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

// IsOK indicates whether or not the first response was successful.
func (r ParsedResponses) IsOK() bool {
	return r.Len() > 0 && r[0].IsOK()
}

// Count occurrences of the given text within the bodies of all responses.
func (r ParsedResponses) Count(text string) int {
	count := 0
	for _, c := range r {
		count += c.Count(text)
	}
	return count
}

// Body concatenates the bodies of all responses.
func (r ParsedResponses) Body() string {
	body := ""
	for _, c := range r {
		body += c.Body
	}
	return body
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

	match = hostnameRegex.FindStringSubmatch(output)
	if match != nil {
		out.Hostname = match[1]
	}

	return &out
}
