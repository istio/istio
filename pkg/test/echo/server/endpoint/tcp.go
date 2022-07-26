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

package endpoint

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/util/retry"
)

var _ Instance = &tcpInstance{}

type tcpInstance struct {
	Config
	l net.Listener
}

func newTCP(config Config) Instance {
	return &tcpInstance{
		Config: config,
	}
}

func (s *tcpInstance) GetConfig() Config {
	return s.Config
}

func (s *tcpInstance) Start(onReady OnReadyFunc) error {
	var listener net.Listener
	var port int
	var err error
	if s.Port.TLS {
		cert, cerr := tls.LoadX509KeyPair(s.TLSCert, s.TLSKey)
		if cerr != nil {
			return fmt.Errorf("could not load TLS keys: %v", cerr)
		}
		config := &tls.Config{Certificates: []tls.Certificate{cert}}
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, port, err = listenOnAddressTLS(s.ListenerIP, s.Port.Port, config)
		// Store the actual listening port back to the argument.
		s.Port.Port = port
	} else {
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, port, err = listenOnAddress(s.ListenerIP, s.Port.Port)
		// Store the actual listening port back to the argument.
		s.Port.Port = port
	}
	if err != nil {
		return err
	}

	s.l = listener
	if s.Port.TLS {
		epLog.Infof("Listening TCP (over TLS) on %v\n", port)
	} else {
		epLog.Infof("Listening TCP on %v\n", port)
	}

	// Start serving TCP traffic.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				epLog.Warn("TCP accept failed: " + err.Error())
				return
			}

			id := uuid.New()
			epLog.WithLabels("remote", conn.RemoteAddr(), "id", id).Infof("TCP Request")

			done := make(chan struct{})
			go func() {
				s.echo(id, conn)
				close(done)
			}()

			go func() {
				select {
				case <-done:
					return
				case <-time.After(requestTimeout):
					epLog.WithLabels("id", id).Warnf("TCP forcing connection closed after request timeout")
					_ = forceClose(conn)
					return
				}
			}()
		}
	}()

	// Notify the WaitGroup once the port has transitioned to ready.
	go s.awaitReady(onReady, listener.Addr().String())

	return nil
}

// Handles incoming connection.
func (s *tcpInstance) echo(id uuid.UUID, conn net.Conn) {
	common.Metrics.TCPRequests.With(common.PortLabel.Value(strconv.Itoa(s.Port.Port))).Increment()

	var err error
	defer func() {
		if err != nil && err != io.EOF {
			_ = forceClose(conn)
		} else {
			_ = conn.Close()
		}
	}()

	// If this is server first, client expects a message from server. Send the magic string.
	if s.Port.ServerFirst {
		if _, err = conn.Write([]byte(common.ServerFirstMagicString)); err != nil {
			epLog.WithLabels("id", id).Warnf("TCP server-first write failed: %v", err)
			return
		}
	}

	firstReply := true
	responseFields := ""
	buf := make([]byte, 4096)
	for {
		var n int
		n, err = conn.Read(buf)

		// important not to start sending any response until we've started reading the message,
		// otherwise the response could be read when we expect the magic string
		if firstReply {
			responseFields = s.getResponseFields(conn)
			if _, writeErr := conn.Write([]byte(responseFields)); writeErr != nil {
				epLog.WithLabels("id", id).Warnf("TCP failed writing response fields: %v", writeErr)
			}
			firstReply = false
		}

		if err != nil && err != io.EOF {
			epLog.WithLabels("id", id).Warnf("TCP read failed: %v", err)
			break
		}

		// echo the message from the request
		if n > 0 {
			out := buf[:n]
			if _, err = conn.Write(out); err != nil {
				epLog.WithLabels("id", id).Warnf("TCP failed writing echo response: %v", err)
				break
			}
		}

		// Read can return n > 0 with EOF, do this last.
		if err == io.EOF {
			break
		}
	}

	epLog.WithLabels("id", id).Infof("TCP Response Fields:\n%s", responseFields)
}

func (s *tcpInstance) getResponseFields(conn net.Conn) string {
	ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	// Write non-request fields specific to the instance
	respFields := map[echo.Field]string{
		echo.StatusCodeField:     strconv.Itoa(http.StatusOK),
		echo.ClusterField:        s.Cluster,
		echo.IstioVersionField:   s.IstioVersion,
		echo.ServiceVersionField: s.Version,
		echo.ServicePortField:    strconv.Itoa(s.Port.Port),
		echo.IPField:             ip,
		echo.ProtocolField:       "TCP",
	}

	if hostname, err := os.Hostname(); err == nil {
		respFields[echo.HostnameField] = hostname
	}

	var out strings.Builder
	for field, val := range respFields {
		val := fmt.Sprintf("%s=%s\n", string(field), val)
		_, _ = out.WriteString(val)
	}
	return out.String()
}

func (s *tcpInstance) Close() error {
	if s.l != nil {
		_ = s.l.Close()
	}
	return nil
}

func (s *tcpInstance) awaitReady(onReady OnReadyFunc, address string) {
	defer onReady()

	err := retry.UntilSuccess(func() error {
		conn, err := net.Dial("tcp", address)
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close() }()

		// Server is up now, we're ready.
		return nil
	}, retry.Timeout(readyTimeout), retry.Delay(readyInterval))

	if err != nil {
		epLog.Errorf("readiness failed for endpoint %s: %v", address, err)
	} else {
		epLog.Infof("ready for TCP endpoint %s", address)
	}
}
