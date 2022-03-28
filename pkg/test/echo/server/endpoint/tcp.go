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
		fmt.Printf("Listening TCP (over TLS) on %v\n", port)
	} else {
		fmt.Printf("Listening TCP on %v\n", port)
	}

	// Start serving TCP traffic.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				epLog.Warn("TCP accept failed: " + err.Error())
				return
			}

			go s.echo(conn)
		}
	}()

	// Notify the WaitGroup once the port has transitioned to ready.
	go s.awaitReady(onReady, listener.Addr().String())

	return nil
}

// Handles incoming connection.
func (s *tcpInstance) echo(conn net.Conn) {
	defer common.Metrics.TCPRequests.With(common.PortLabel.Value(strconv.Itoa(s.Port.Port))).Increment()
	defer func() {
		_ = conn.Close()
	}()

	// If this is server first, client expects a message from server. Send the magic string.
	if s.Port.ServerFirst {
		_, _ = conn.Write([]byte(common.ServerFirstMagicString))
	}

	id := uuid.New()
	epLog.WithLabels("remote", conn.RemoteAddr(), "id", id).Infof("TCP Request")
	firstReply := true
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)

		// important not to start sending any response until we've started reading the message,
		// otherwise the response could be read when we expect the magic string
		if firstReply {
			s.writeResponse(conn)
			firstReply = false
		}

		if err != nil && err != io.EOF {
			epLog.Warnf("TCP read failed: %v", err.Error())
			break
		}

		// echo the message from the request
		if n > 0 {
			out := buf[:n]
			if _, err := conn.Write(out); err != nil {
				epLog.Warnf("TCP write failed, :%v", err)
				break
			}
		}

		// Read can return n > 0 with EOF, do this last.
		if err == io.EOF {
			break
		}
	}

	epLog.WithLabels("id", id).Infof("TCP Response")
}

func (s *tcpInstance) writeResponse(conn net.Conn) {
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
	for field, val := range respFields {
		val := fmt.Sprintf("%s=%s\n", string(field), val)
		_, err := conn.Write([]byte(val))
		if err != nil {
			epLog.Warnf("TCP write failed %q: %v", val, err)
			break
		}
	}
}

func (s *tcpInstance) Close() error {
	if s.l != nil {
		s.l.Close()
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
		defer conn.Close()

		// Server is up now, we're ready.
		return nil
	}, retry.Timeout(readyTimeout), retry.Delay(readyInterval))

	if err != nil {
		epLog.Errorf("readiness failed for endpoint %s: %v", address, err)
	} else {
		epLog.Infof("ready for TCP endpoint %s", address)
	}
}
