// Copyright 2017 Istio Authors
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

package echo

import (
	"bytes"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"istio.io/istio/tests/e2e/apps/hop/config"
)

var (
	timeout = flag.Duration("timeout", 15*time.Second, "Request timeout")
)

func newHopMessage(u *[]string) *config.HopMessage {
	r := new(config.HopMessage)
	if u != nil {
		for _, d := range *u {
			dest := new(config.Remote)
			dest.Destination = d
			r.RemoteDests = append(r.RemoteDests, dest)
		}
	}
	return r
}

// NewApp creates a new Hop App with default settings
func NewApp() *App {
	a := new(App)
	a.clientTimeout = *timeout
	a.marshaller = jsonpb.Marshaler{}
	a.unmarshaller = jsonpb.Unmarshaler{}
	/* #nosec */
	a.httpClient = http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: a.clientTimeout,
	}
	return a
}

// App contains both server and client for Hop App
type App struct {
	marshaller    jsonpb.Marshaler
	unmarshaller  jsonpb.Unmarshaler
	httpClient    http.Client
	clientTimeout time.Duration
}

func (a App) makeHTTPRequest(m *config.HopMessage, url string) (*config.HopMessage, error) {
	var err error
	var jsonStr string
	var req *http.Request
	if jsonStr, err = a.marshaller.MarshalToString(m); err != nil {
		if req, err = http.NewRequest("POST", url, bytes.NewBuffer([]byte(jsonStr))); err == nil {
			req.Header.Set("X-Custom-Header", "myvalue")
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{}
			var resp *http.Response
			if resp, err = client.Do(req); err == nil {
				pb := new(config.HopMessage)
				if err = a.unmarshaller.Unmarshal(resp.Body, pb); err == nil {
					err = resp.Body.Close()
					return pb, err
				}
				e := resp.Body.Close()
				glog.Error(e.Error())
				return pb, err
			}
		}
	}
	return nil, err
}

func (a App) makeGRPCRequest(m *config.HopMessage, address string) (*config.HopMessage, error) {
	var err error
	var conn *grpc.ClientConn
	if conn, err = grpc.Dial(address,
		grpc.WithInsecure(),
		// grpc-go sets incorrect authority header
		grpc.WithAuthority(address),
		grpc.WithBlock(),
		grpc.WithTimeout(a.clientTimeout)); err == nil {
		client := config.NewHopTestServiceClient(conn)
		var resp *config.HopMessage
		if resp, err = client.Hop(context.Background(), m); err == nil {
			err = conn.Close()
			return resp, err
		}
	}
	if conn != nil {
		err = conn.Close()
		glog.Error(err.Error())
	}
	return nil, err
}

// ServerHTTP starts a HTTP Server for Hop App
func (a App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := new(config.HopMessage)
	err := a.unmarshaller.Unmarshal(r.Body, req)
	if err == nil {
		resp := a.forwardMessage(req)
		if resp.GetError() != "" {
			err = fmt.Errorf(resp.GetError())
		} else {
			var jsonStr string
			jsonStr, err = a.marshaller.MarshalToString(resp)
			if err == nil {
				_, err = w.Write([]byte(jsonStr))
				if err == nil {
					w.WriteHeader(http.StatusOK)
				}

			}

		}
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// Hop start a gRPC server for Hop App
func (a App) Hop(ctx context.Context, req *config.HopMessage) (*config.HopMessage, error) {
	var err error
	resp := a.forwardMessage(req)
	if resp.GetError() != "" {
		err = fmt.Errorf(resp.GetError())
	} else {
		err = nil
	}
	return resp, err
}

func (a App) makeRequest(m *config.HopMessage, d string) (*config.HopMessage, error) {
	// Check destination and send using grpc or http handler
	if strings.HasPrefix(d, "http://") {
		return a.makeHTTPRequest(m, d)

	} else if strings.HasPrefix(d, "grpc://") {
		address := d[len("grpc://"):]
		return a.makeGRPCRequest(m, address)
	}
	return nil, errors.New("protocol not supported")
}

func (a App) nextDestination(m *config.HopMessage) (int, string) {
	if m.GetError() != "" {
		// Error along the way no need to continue
		return -1, ""
	}
	for i, remote := range m.GetRemoteDests() {
		if remote.GetDone() {
			continue
		}
		return i, remote.GetDestination()
	}
	return -1, ""
}

// MakeRequest will create a config.HopMessage proto and
// send requests to defined remotes hosts in a chain.
// Each Server is a client and will forward the call to the next hop.
func (a App) MakeRequest(remotes *[]string) error {
	req := newHopMessage(remotes)
	resp := a.forwardMessage(req)
	if resp.GetError() != "" {
		return nil
	}
	return errors.New(resp.GetError())
}

func (a App) forwardMessage(req *config.HopMessage) *config.HopMessage {
	i, d := a.nextDestination(req)
	if i > 0 {
		glog.Info("Sending message %s to ")
		startTime := time.Now()
		resp, err := a.makeRequest(req, d)
		rtt := time.Since(startTime)
		if resp != nil {
			a.updateMessage(resp, i, rtt, err)
			return resp
		}
		a.updateMessage(req, i, rtt, err)
	}
	return req
}

func (a App) updateMessage(m *config.HopMessage, index int, rtt time.Duration, err error) {
	if len(m.RemoteDests) > index {
		if err != nil {
			m.Error = err.Error()
		}
		if !m.RemoteDests[index].Done {
			m.RemoteDests[index].Done = true
			m.RemoteDests[index].Rtt = rtt
		}
		m.Error = "already went over this destination"
	}
	m.Error = "no index found for this destination"
}
