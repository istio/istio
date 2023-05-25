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
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"istio.io/istio/pkg/test/echo"
	"istio.io/istio/pkg/test/util/retry"
)

var _ Instance = &udpInstance{}

type udpInstance struct {
	Config
	l net.PacketConn
}

func newUDP(config Config) Instance {
	return &udpInstance{
		Config: config,
	}
}

func (s *udpInstance) GetConfig() Config {
	return s.Config
}

func (s *udpInstance) Start(onReady OnReadyFunc) error {
	var listener net.PacketConn
	var port int
	var err error
	if s.Port.TLS {
		return fmt.Errorf("TLS not supported for UDP")
	}
	// Listen on the given port and update the port if it changed from what was passed in.
	listener, port, err = listenUDPAddress(s.ListenerIP, s.Port.Port)
	// Store the actual listening port back to the argument.
	s.Port.Port = port
	if err != nil {
		return err
	}

	s.l = listener
	epLog.Infof("Listening UDP on %v\n", port)

	// Start serving UDP traffic.
	go func() {
		buf := make([]byte, 2048)
		for {
			_, remote, err := listener.ReadFrom(buf)
			if err != nil {
				epLog.Warn("UDP read failed: " + err.Error())
				return
			}

			id := uuid.New()
			epLog.WithLabels("remote", remote, "id", id).Infof("UDP Request")

			responseFields := s.getResponseFields(remote)
			if _, err := listener.WriteTo([]byte(responseFields), remote); err != nil {
				epLog.WithLabels("id", id).Warnf("UDP failed writing echo response: %v", err)
			}
		}
	}()

	// Notify the WaitGroup once the port has transitioned to ready.
	go s.awaitReady(onReady, listener.LocalAddr().String())
	return nil
}

func (s *udpInstance) getResponseFields(conn net.Addr) string {
	ip, _, _ := net.SplitHostPort(conn.String())
	// Write non-request fields specific to the instance
	out := &strings.Builder{}
	echo.StatusCodeField.Write(out, strconv.Itoa(http.StatusOK))
	echo.ClusterField.WriteNonEmpty(out, s.Cluster)
	echo.IstioVersionField.WriteNonEmpty(out, s.IstioVersion)
	echo.NamespaceField.WriteNonEmpty(out, s.Namespace)
	echo.ServiceVersionField.Write(out, s.Version)
	echo.ServicePortField.Write(out, strconv.Itoa(s.Port.Port))
	echo.IPField.Write(out, ip)
	echo.ProtocolField.Write(out, "TCP")

	if hostname, err := os.Hostname(); err == nil {
		echo.HostnameField.Write(out, hostname)
	}
	return out.String()
}

func (s *udpInstance) Close() error {
	if s.l != nil {
		_ = s.l.Close()
	}
	return nil
}

func (s *udpInstance) awaitReady(onReady OnReadyFunc, address string) {
	defer onReady()

	err := retry.UntilSuccess(func() error {
		conn, err := net.Dial("udp", address)
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
		epLog.Infof("ready for UDP endpoint %s", address)
	}
}
