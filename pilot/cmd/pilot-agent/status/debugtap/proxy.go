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

package debugtap

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/log"
)

type Client interface {
	DebugTapRequest(req *discovery.DiscoveryRequest, timeout time.Duration) (*discovery.DiscoveryResponse, error)
}

type Proxy struct {
	client Client
}

func NewProxy(client Client) *Proxy {
	return &Proxy{client: client}
}

func (p *Proxy) RegisterGRPCHandler(grpcs *grpc.Server) {
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcs, p)
	reflection.Register(grpcs)
}

func (p *Proxy) RegisterHTTPHandler(httpMux *http.ServeMux) {
	handler := p.makeTapHTTPHandler()
	httpMux.HandleFunc("/debug/", handler)
	httpMux.HandleFunc("/debug", handler) // For 1.10 Istiod which uses istio.io/debug
}

func (p *Proxy) makeTapHTTPHandler() func(w http.ResponseWriter, req *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		qp, err := url.ParseQuery(req.URL.RawQuery)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "%v\n", err)
			return
		}
		// Strip prefix: /tap/debug... -> /debug...
		typeURL := fmt.Sprintf("istio.io%s", req.URL.Path)
		dr := discovery.DiscoveryRequest{
			TypeUrl: typeURL,
		}
		resourceName := qp.Get("resourceName")
		if resourceName != "" {
			dr.ResourceNames = []string{resourceName}
		}
		response, err := p.client.DebugTapRequest(&dr, 5*time.Second)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "%v\n", err)
			return
		}

		if response == nil {
			log.Infof("timed out waiting for Istiod to respond to %q", typeURL)
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}

		// Try to unmarshal Istiod's response using protojson (needed for Envoy protobufs)
		w.Header().Add("Content-Type", "application/json")
		b, err := protomarshal.MarshalIndent(response, "  ")
		if err == nil {
			_, err = w.Write(b)
			if err != nil {
				log.Infof("fail to write debug response: %v", err)
			}
			return
		}

		// Failed as protobuf.  Try as regular JSON
		log.Warnf("could not marshal istiod response as pb: %v", err)
		j, err := json.Marshal(response)
		if err != nil {
			// Couldn't unmarshal at all
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "%v\n", err)
			return
		}
		_, err = w.Write(j)
		if err != nil {
			log.Infof("fail to write debug response: %v", err)
			return
		}
	}
}

func (p *Proxy) StreamAggregatedResources(downstream xds.DiscoveryStream) error {
	timeout := time.Second * 15
	req, err := downstream.Recv()
	if err != nil {
		log.Errorf("failed to recv: %v", err)
		return err
	}
	if strings.HasPrefix(req.TypeUrl, xds.TypeDebugPrefix) {
		if resp, err := p.client.DebugTapRequest(req, timeout); err == nil {
			err := downstream.Send(resp)
			if err != nil {
				log.Errorf("failed to send: %v", err)
				return err
			}
		} else {
			log.Errorf("failed to call tap request: %v", err)
			return err
		}
	}
	return nil
}

func (p *Proxy) DeltaAggregatedResources(downstream xds.DeltaDiscoveryStream) error {
	return fmt.Errorf("not implemented")
}
