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
	"net"
	"net/http"

	"istio.io/istio/pkg/hbone"
)

var _ Instance = &connectInstance{}

type connectInstance struct {
	Config
	server *http.Server
}

func newHBONE(config Config) Instance {
	return &connectInstance{
		Config: config,
	}
}

func (c connectInstance) Close() error {
	if c.server != nil {
		return c.server.Close()
	}
	return nil
}

func (c connectInstance) Start(onReady OnReadyFunc) error {
	defer onReady()
	c.server = hbone.NewServer()

	var listener net.Listener
	var port int
	var err error
	if c.Port.TLS {
		cert, cerr := tls.LoadX509KeyPair(c.TLSCert, c.TLSKey)
		if cerr != nil {
			return fmt.Errorf("could not load TLS keys: %v", cerr)
		}
		config := &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2"},
			GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
				// There isn't a way to pass through all ALPNs presented by the client down to the
				// HTTP server to return in the response. However, for debugging, we can at least log
				// them at this level.
				epLog.Infof("TLS connection with alpn: %v", info.SupportedProtos)
				return nil, nil
			},
		}
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, port, err = listenOnAddressTLS(c.ListenerIP, c.Port.Port, config)
		// Store the actual listening port back to the argument.
		c.Port.Port = port
	} else {
		// Listen on the given port and update the port if it changed from what was passed in.
		listener, port, err = listenOnAddress(c.ListenerIP, c.Port.Port)
		// Store the actual listening port back to the argument.
		c.Port.Port = port
	}
	if err != nil {
		return err
	}

	if c.Port.TLS {
		c.server.Addr = fmt.Sprintf(":%d", port)
		epLog.Infof("Listening HBONE on %v\n", port)
	} else {
		c.server.Addr = fmt.Sprintf(":%d", port)
		epLog.Infof("Listening HBONE (plaintext) on %v\n", port)
	}
	go func() {
		err := c.server.Serve(listener)
		epLog.Warnf("Port %d listener terminated with error: %v", port, err)
	}()
	return nil
}

func (c connectInstance) GetConfig() Config {
	return c.Config
}
