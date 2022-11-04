// Copyright Istio Authors
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

package forwarder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
)

var _ protocol = &udpProtocol{}

type udpProtocol struct {
	e *executor
}

func newUDPProtocol(e *executor) protocol {
	return &udpProtocol{e: e}
}

func (c *udpProtocol) ForwardEcho(ctx context.Context, cfg *Config) (*proto.ForwardEchoResponse, error) {
	return doForward(ctx, cfg, c.e, c.makeRequest)
}

func (c *udpProtocol) makeRequest(ctx context.Context, cfg *Config, requestID int) (string, error) {
	conn, err := newUDPConnection(cfg)
	if err != nil {
		return "", err
	}
	defer func() { _ = conn.Close() }()

	msgBuilder := strings.Builder{}
	echo.ForwarderURLField.WriteForRequest(&msgBuilder, requestID, cfg.Request.Url)

	if cfg.Request.Message != "" {
		echo.ForwarderMessageField.WriteForRequest(&msgBuilder, requestID, cfg.Request.Message)
	}

	// Apply per-request timeout to calculate deadline for reads/writes.
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	// Apply the deadline to the connection.
	deadline, _ := ctx.Deadline()
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return msgBuilder.String(), err
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return msgBuilder.String(), err
	}

	// Make sure the client writes something to the buffer
	message := "HelloWorld"
	if cfg.Request.Message != "" {
		message = cfg.Request.Message
	}

	if _, err := conn.Write([]byte(message + "\n")); err != nil {
		fwLog.Warnf("UDP write failed: %v", err)
		return msgBuilder.String(), err
	}
	var resBuffer bytes.Buffer
	buf := make([]byte, 1024+len(message))
	n, err := conn.Read(buf)
	if err != nil && err != io.EOF {
		fwLog.Warnf("UDP read failed (already read %d bytes): %v", len(resBuffer.String()), err)
		return msgBuilder.String(), err
	}
	resBuffer.Write(buf[:n])

	// format the output for forwarder response
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		if line != "" {
			echo.WriteBodyLine(&msgBuilder, requestID, line)
		}
	}

	msg := msgBuilder.String()
	expected := fmt.Sprintf("%s=%d", string(echo.StatusCodeField), http.StatusOK)
	if cfg.Request.ExpectedResponse != nil {
		expected = cfg.Request.ExpectedResponse.GetValue()
	}
	if !strings.Contains(msg, expected) {
		return msg, fmt.Errorf("expect to recv message with %s, got %s. Return EOF", expected, msg)
	}
	return msg, nil
}

func (c *udpProtocol) Close() error {
	return nil
}

func newUDPConnection(cfg *Config) (net.Conn, error) {
	address := cfg.Request.Url[len(cfg.scheme+"://"):]

	if cfg.secure {
		return nil, fmt.Errorf("TLS not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), common.ConnectionTimeout)
	defer cancel()
	return newDialer(cfg).DialContext(ctx, "udp", address)
}
