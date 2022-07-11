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
	"net"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/proto"
)

var _ protocol = &websocketProtocol{}

type websocketProtocol struct {
	e *executor
}

func newWebsocketProtocol(e *executor) protocol {
	return &websocketProtocol{e: e}
}

func (c *websocketProtocol) ForwardEcho(ctx context.Context, cfg *Config) (*proto.ForwardEchoResponse, error) {
	return doForward(ctx, cfg, c.e, c.makeRequest)
}

func (c *websocketProtocol) Close() error {
	return nil
}

func (c *websocketProtocol) makeRequest(ctx context.Context, cfg *Config, requestID int) (string, error) {
	req := cfg.Request
	var outBuffer bytes.Buffer
	echo.ForwarderURLField.WriteForRequest(&outBuffer, requestID, req.Url)

	// Set the special header to trigger the upgrade to WebSocket.
	wsReq := cfg.headers.Clone()
	if len(cfg.hostHeader) > 0 {
		echo.HostField.WriteForRequest(&outBuffer, requestID, hostHeader)
	}
	writeForwardedHeaders(&outBuffer, requestID, wsReq)
	common.SetWebSocketHeader(wsReq)

	if req.Message != "" {
		echo.ForwarderMessageField.WriteForRequest(&outBuffer, requestID, req.Message)
	}

	dialContext := func(network, addr string) (net.Conn, error) {
		return newDialer(cfg).Dial(network, addr)
	}
	if len(cfg.UDS) > 0 {
		dialContext = func(network, addr string) (net.Conn, error) {
			return newDialer(cfg).Dial("unix", cfg.UDS)
		}
	}

	dialer := &websocket.Dialer{
		TLSClientConfig:  cfg.tlsConfig,
		NetDial:          dialContext,
		HandshakeTimeout: cfg.timeout,
	}

	conn, _, err := dialer.Dial(req.Url, wsReq)
	if err != nil {
		// timeout or bad handshake
		return outBuffer.String(), err
	}
	defer func() {
		_ = conn.Close()
	}()

	// Apply per-request timeout to calculate deadline for reads/writes.
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	// Apply the deadline to the connection.
	deadline, _ := ctx.Deadline()
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return outBuffer.String(), err
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return outBuffer.String(), err
	}

	start := time.Now()
	err = conn.WriteMessage(websocket.TextMessage, []byte(req.Message))
	if err != nil {
		return outBuffer.String(), err
	}

	_, resp, err := conn.ReadMessage()
	if err != nil {
		return outBuffer.String(), err
	}

	echo.LatencyField.WriteForRequest(&outBuffer, requestID, fmt.Sprintf("%v", time.Since(start)))
	echo.ActiveRequestsField.WriteForRequest(&outBuffer, requestID, fmt.Sprintf("%d", c.e.ActiveRequests()))
	for _, line := range strings.Split(string(resp), "\n") {
		if line != "" {
			echo.WriteBodyLine(&outBuffer, requestID, line)
		}
	}

	return outBuffer.String(), nil
}
