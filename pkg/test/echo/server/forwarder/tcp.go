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
	"net"
	"strings"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/echo/common/response"
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

	var outBuffer bytes.Buffer
	outBuffer.WriteString(fmt.Sprintf("[%d] Url=%s\n", req.RequestID, req.URL))

	if req.Message != "" {
		outBuffer.WriteString(fmt.Sprintf("[%d] Echo=%s\n", req.RequestID, req.Message))
	}

	// Apply per-request timeout to calculate deadline for reads/writes.
	ctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	// Apply the deadline to the connection.
	deadline, _ := ctx.Deadline()
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return outBuffer.String(), err
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return outBuffer.String(), err
	}

	// For server first protocol, we expect the server to send us the magic string first
	if req.ServerFirst {
		bytes, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
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
		return outBuffer.String(), err
	}

	buf := make([]byte, 1024+len(message))
	n, err := bufio.NewReader(conn).Read(buf)
	if err != nil {
		return outBuffer.String(), err
	}

	for _, line := range strings.Split(string(buf[:n]), "\n") {
		if line != "" {
			outBuffer.WriteString(fmt.Sprintf("[%d body] %s\n", req.RequestID, line))
		}
	}

	msg := outBuffer.String()
	expected := fmt.Sprintf("%s=%s", string(response.StatusCodeField), response.StatusCodeOK)
	if !strings.Contains(msg, expected) {
		return msg, fmt.Errorf("expect to recv message with %s, got %s. Return EOF", expected, msg)
	}
	return outBuffer.String(), err
}

func (c *tcpProtocol) Close() error {
	return nil
}
