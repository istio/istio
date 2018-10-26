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

// An example implementation of a client.

package echo

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"

	"istio.io/istio/pkg/test/application"
	"istio.io/istio/pkg/test/application/echo/proto"
)

const (
	hostKey = "Host"
)

type request struct {
	URL       string
	Header    http.Header
	RequestID int
	Message   string
}

type response string

type protocol interface {
	makeRequest(req *request) (response, error)
	Close() error
}

type httpProtocol struct {
	client *http.Client
	do     application.HTTPDoFunc
}

func (c *httpProtocol) setHost(r *http.Request, host string) {
	r.Host = host

	if r.URL.Scheme == "https" {
		// Set SNI value to be same as the request Host
		// For use with SNI routing tests
		var httpTransport *http.Transport
		httpTransport = c.client.Transport.(*http.Transport)
		httpTransport.TLSClientConfig.ServerName = host
	}
}

func (c *httpProtocol) makeRequest(req *request) (response, error) {
	httpReq, err := http.NewRequest("GET", req.URL, nil)
	if err != nil {
		return "", err
	}

	var outBuffer bytes.Buffer
	outBuffer.WriteString(fmt.Sprintf("[%d] Url=%s\n", req.RequestID, req.URL))
	host := ""
	writeHeaders(req.RequestID, req.Header, outBuffer, func(key string, value string) {
		if key == hostKey {
			host = value
		} else {
			httpReq.Header.Add(key, value)
		}
	})

	c.setHost(httpReq, host)

	httpResp, err := c.do(c.client, httpReq)
	if err != nil {
		return response(outBuffer.String()), err
	}

	outBuffer.WriteString(fmt.Sprintf("[%d] StatusCode=%d\n", req.RequestID, httpResp.StatusCode))

	for key, values := range httpResp.Header {
		for _, value := range values {
			outBuffer.WriteString(fmt.Sprintf("[%d] ResponseHeader=%s:%s\n", req.RequestID, key, value))
		}
	}

	data, err := ioutil.ReadAll(httpResp.Body)
	defer func() {
		if err = httpResp.Body.Close(); err != nil {
			outBuffer.WriteString(fmt.Sprintf("[%d error] %s\n", req.RequestID, err))
		}
	}()

	if err != nil {
		return response(outBuffer.String()), err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			outBuffer.WriteString(fmt.Sprintf("[%d body] %s\n", req.RequestID, line))
		}
	}

	return response(outBuffer.String()), nil
}

func (c *httpProtocol) Close() error {
	return nil
}

type grpcProtocol struct {
	conn   *grpc.ClientConn
	client proto.EchoTestServiceClient
}

func (c *grpcProtocol) makeRequest(req *request) (response, error) {
	grpcReq := &proto.EchoRequest{
		Message: fmt.Sprintf("request #%d", req.RequestID),
	}
	log.Printf("[%d] grpcecho.Echo(%v)\n", req.RequestID, req)
	resp, err := c.client.Echo(context.Background(), grpcReq)
	if err != nil {
		return "", err
	}

	// when the underlying HTTP2 request returns status 404, GRPC
	// request does not return an error in grpc-go.
	// instead it just returns an empty response
	var outBuffer bytes.Buffer
	for _, line := range strings.Split(resp.GetMessage(), "\n") {
		if line != "" {
			log.Printf("[%d body] %s\n", req.RequestID, line)
		}
	}
	return response(outBuffer.String()), nil
}

func (c *grpcProtocol) Close() error {
	return c.conn.Close()
}

type websocketProtocol struct {
	dialer *websocket.Dialer
}

func (c *websocketProtocol) makeRequest(req *request) (response, error) {
	wsReq := make(http.Header)

	var outBuffer bytes.Buffer
	outBuffer.WriteString(fmt.Sprintf("[%d] Url=%s\n", req.RequestID, req.URL))
	writeHeaders(req.RequestID, req.Header, outBuffer, wsReq.Add)

	if req.Message != "" {
		outBuffer.WriteString(fmt.Sprintf("[%d] Body=%s\n", req.RequestID, req.Message))
	}

	conn, _, err := c.dialer.Dial(req.URL, wsReq)
	if err != nil {
		// timeout or bad handshake
		return response(outBuffer.String()), err
	}
	// nolint: errcheck
	defer conn.Close()

	err = conn.WriteMessage(websocket.TextMessage, []byte(req.Message))
	if err != nil {
		return response(outBuffer.String()), err
	}

	_, resp, err := conn.ReadMessage()
	if err != nil {
		return response(outBuffer.String()), err
	}

	for _, line := range strings.Split(string(resp), "\n") {
		if line != "" {
			outBuffer.WriteString(fmt.Sprintf("[%d body] %s\n", req.RequestID, line))
		}
	}

	return response(outBuffer.String()), nil
}

func (c *websocketProtocol) Close() error {
	return nil
}

func writeHeaders(requestID int, header http.Header, outBuffer bytes.Buffer, addFn func(string, string)) {
	for key, values := range header {
		key = textproto.CanonicalMIMEHeaderKey(key)
		for _, v := range values {
			addFn(key, v)
			if key == hostKey {
				outBuffer.WriteString(fmt.Sprintf("[%d] Host=%s\n", requestID, v))
			} else {
				outBuffer.WriteString(fmt.Sprintf("[%d] Header=%s:%s\n", requestID, key, v))
			}
		}
	}
}
