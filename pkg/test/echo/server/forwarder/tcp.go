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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
)

var _ protocol = &tcpProtocol{}

type tcpProtocol struct {
	// conn returns a new connection. This is not just a shared connection as we will
	// not re-use the connection for multiple requests with TCP
	conn func() (net.Conn, error)
}

func (c *tcpProtocol) makeRequest(ctx context.Context, req *request) (string, error) {
	conn, err := c.conn()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	msgBuilder := strings.Builder{}
	msgBuilder.WriteString(fmt.Sprintf("[%d] Url=%s\n", req.RequestID, req.URL))

	if req.Message != "" {
		msgBuilder.WriteString(fmt.Sprintf("[%d] Echo=%s\n", req.RequestID, req.Message))
	}

	// Apply per-request timeout to calculate deadline for reads/writes.
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	// Apply the deadline to the connection.
	deadline, _ := ctx.Deadline()
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return msgBuilder.String(), err
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return msgBuilder.String(), err
	}

	// For server first protocol, we expect the server to send us the magic string first
	if req.ServerFirst {
		bytes, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			fwLog.Warnf("server first TCP read failed: %v", err)
			return "", err
		}
		if string(bytes) != common.ServerFirstMagicString {
			return "", fmt.Errorf("did not receive magic sting. Want %q, got %q", common.ServerFirstMagicString, string(bytes))
		}
	}

	// Make sure the client writes something to the buffer
	message := "HelloWorld"
	if req.Message != "" {
		message = req.Message
	}

	if _, err := conn.Write([]byte(message + "\n")); err != nil {
		fwLog.Warnf("TCP write failed: %v", err)
		return msgBuilder.String(), err
	}
	var resBuffer bytes.Buffer
	buf := make([]byte, 1024+len(message))
	for {
		n, err := conn.Read(buf)
		if err != nil && err != io.EOF {
			fwLog.Warnf("TCP read failed (already read %d bytes): %v", len(resBuffer.String()), err)
			return msgBuilder.String(), err
		}
		resBuffer.Write(buf[:n])
		// the message is sent last - when we get the whole message we can stop reading
		if err == io.EOF || strings.Contains(resBuffer.String(), message) {
			break
		}
	}

	// format the output for forwarder response
	for _, line := range strings.Split(resBuffer.String(), "\n") {
		if line != "" {
			msgBuilder.WriteString(fmt.Sprintf("[%d body] %s\n", req.RequestID, line))
		}
	}

	msg := msgBuilder.String()
	expected := fmt.Sprintf("%s=%d", string(echo.StatusCodeField), http.StatusOK)
	if req.ExpectedResponse != nil {
		expected = req.ExpectedResponse.GetValue()
	}
	if !strings.Contains(msg, expected) {
		return msg, fmt.Errorf("expect to recv message with %s, got %s. Return EOF", expected, msg)
	}
	return msg, nil
}

func (c *tcpProtocol) Close() error {
	return nil
}
