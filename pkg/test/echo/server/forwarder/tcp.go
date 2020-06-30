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

	"istio.io/istio/pkg/test/echo/common/response"
)

var _ protocol = &tcpProtocol{}

type tcpProtocol struct {
	conn net.Conn
}

func (c *tcpProtocol) makeRequest(ctx context.Context, req *request) (string, error) {
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
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return outBuffer.String(), err
	}
	if err := c.conn.SetReadDeadline(deadline); err != nil {
		return outBuffer.String(), err
	}

	// Make sure the client writes something to the buffer
	message := "HelloWorld"
	if req.Message != "" {
		message = req.Message
	}

	_, err := c.conn.Write([]byte(message + "\n"))
	if err != nil {
		return outBuffer.String(), err
	}

	buf := make([]byte, 1024+len(message))
	n, err := bufio.NewReader(c.conn).Read(buf)
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
	c.conn.Close()
	return nil
}
