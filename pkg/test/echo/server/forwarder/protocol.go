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

// An example implementation of a client.

package forwarder

import (
	"context"
	"fmt"
	"net/http"
	"time"

	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	"istio.io/istio/pkg/test/echo/common/scheme"
)

type request struct {
	URL              string
	Header           http.Header
	RequestID        int
	Message          string
	ExpectedResponse *wrappers.StringValue
	Timeout          time.Duration
	ServerFirst      bool
	Method           string
}

type protocol interface {
	makeRequest(ctx context.Context, req *request) (string, error)
	Close() error
}

func newProtocol(cfg *Config) (protocol, error) {
	switch cfg.scheme {
	case scheme.HTTP, scheme.HTTPS:
		return newHTTPProtocol(cfg)
	case scheme.GRPC:
		return newGRPCProtocol(cfg)
	case scheme.XDS:
		return newXDSProtocol(cfg)
	case scheme.WebSocket:
		return newWebsocketProtocol(cfg)
	case scheme.DNS:
		return &dnsProtocol{}, nil
	case scheme.TCP:
		return newTCPProtocol(cfg)
	case scheme.TLS:
		return newTLSProtocol(cfg)
	default:
		return nil, fmt.Errorf("unrecognized protocol %q", cfg.scheme)
	}
}
