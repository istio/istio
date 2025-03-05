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
	"io"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
)

// IsServerReadyFunc is a function that indicates whether the server is currently ready to handle traffic.
type IsServerReadyFunc func() bool

// OnReadyFunc is a callback function that informs the server that the endpoint is ready.
type OnReadyFunc func()

// Config for a single endpoint Instance.
type Config struct {
	IsServerReady IsServerReadyFunc
	Version       string
	Cluster       string
	TLSCert       string
	TLSKey        string
	UDSServer     string
	Dialer        common.Dialer
	Port          *common.Port
	ListenerIP    string
	IstioVersion  string
	Namespace     string
	DisableALPN   bool
	ReportRequest func()
}

// Instance of an endpoint that serves the Echo application on a single port/protocol.
type Instance interface {
	io.Closer
	Start(onReady OnReadyFunc) error
	GetConfig() Config
}

// New creates a new endpoint Instance.
func New(cfg Config) (Instance, error) {
	if cfg.Port != nil {
		switch cfg.Port.Protocol {
		case protocol.HBONE:
			return newHBONE(cfg), nil
		case protocol.DoubleHBONE:
			return newDoubleHBONE(cfg), nil
		case protocol.HTTP, protocol.HTTPS:
			return newHTTP(cfg), nil
		case protocol.HTTP2, protocol.GRPC:
			return newGRPC(cfg), nil
		case protocol.TCP:
			return newTCP(cfg), nil
		case protocol.UDP:
			return newUDP(cfg), nil
		default:
			return nil, fmt.Errorf("unsupported protocol: %s", cfg.Port.Protocol)
		}
	}

	if len(cfg.UDSServer) > 0 {
		return newHTTP(cfg), nil
	}

	return nil, fmt.Errorf("either port or UDS must be specified")
}
