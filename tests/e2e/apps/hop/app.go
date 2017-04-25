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

package hop

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
	"github.com/google/uuid"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"istio.io/istio/tests/e2e/apps/hop/config"
)

var (
	timeout = flag.Duration("timeout", 15*time.Second, "Request timeout")
)

func newHopMessage(u *[]string) *config.HopMessage {
	r := new(config.HopMessage)
	r.Id = uuid.New().String()
	if u != nil {
		for _, d := range *u {
			dest := new(config.Remote)
			dest.Destination = d
			r.RemoteDests = append(r.RemoteDests, dest)
		}
	}
	glog.Infof("Created HOP with id %s", r.GetId())
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
	glog.Infof("Making HTTP Request to %s", url)
	jsonStr, err := a.marshaller.MarshalToString(m)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(jsonStr)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Custom-Header", "myvalue")
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	defer func() {
		if e := resp.Body.Close(); e != nil {
			glog.Error(err)
		}
	}()
	if err != nil {
		return nil, err
	}
	pb := new(config.HopMessage)
	if err = a.unmarshaller.Unmarshal(resp.Body, pb); err != nil {
		return nil, err
	}
	return pb, err
}

func (a App) makeGRPCRequest(req *config.HopMessage, address string) (*config.HopMessage, error) {
	glog.Infof("Making GRPC Request to %s", address)
	conn, err := grpc.Dial(address,
		grpc.WithInsecure(),
		// grpc-go sets incorrect authority header
		grpc.WithAuthority(address),
		grpc.WithBlock(),
		grpc.WithTimeout(a.clientTimeout))
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := conn.Close(); err != nil {
			glog.Error(e)
		}
	}()
	client := config.NewHopTestServiceClient(conn)
	resp, err := client.Hop(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ServerHTTP starts a HTTP Server for Hop App
func (a App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := new(config.HopMessage)
	err := a.unmarshaller.Unmarshal(r.Body, req)
	if err != nil {
		glog.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p := req.GetPosition()
	glog.Infof("HTTP Serving message %s at position %d", req.GetId(), p)
	resp := a.forwardMessage(req)
	if resp.GetError() != "" {
		err = fmt.Errorf(resp.GetError())
		glog.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonStr, err := a.marshaller.MarshalToString(resp)
	if err != nil {
		glog.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, err = w.Write([]byte(jsonStr))
	if err != nil {
		glog.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	glog.Infof("Successfully served message %s at position %d", req.GetId(), p)
}

// Hop start a gRPC server for Hop App
func (a App) Hop(ctx context.Context, req *config.HopMessage) (*config.HopMessage, error) {
	var err error
	glog.Infof("GRPC Serving message %s", req.GetId())
	resp := a.forwardMessage(req)
	if resp.GetError() != "" {
		err = fmt.Errorf(resp.GetError())
		return resp, err
	}
	return resp, nil
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

func (a App) setNextPosition(m *config.HopMessage) {
	if m.GetError() != "" {
		// Error along the way no need to continue
		m.Position = -1
		return
	}
	if m.GetPosition() < 0 {
		return
	}
	nextPos := m.GetPosition() + 1
	if nextPos < int64(len(m.GetRemoteDests())) {
		m.Position = nextPos
		d := m.GetRemoteDests()[nextPos].GetDestination()
		glog.Infof("Message %s next destination is %s", m.GetId(), d)
	} else {
		m.Position = -1
	}

}

// MakeRequest will create a config.HopMessage proto and
// send requests to defined remotes hosts in a chain.
// Each Server is a client and will forward the call to the next hop.
func (a App) MakeRequest(remotes *[]string) (*config.HopMessage, error) {
	req := newHopMessage(remotes)
	resp := a.forwardMessage(req)
	if resp.GetError() == "" {
		return resp, nil
	}
	return resp, errors.New(resp.GetError())
}

func (a App) forwardMessage(m *config.HopMessage) *config.HopMessage {
	p := m.GetPosition()
	glog.Infof("Message %s current position is %d", m.GetId(), p)
	a.setNextPosition(m)
	if p >= 0 {
		d := m.GetRemoteDests()[p].GetDestination()
		glog.Infof("Forwarding message %s to %s", m.GetId(), d)
		startTime := time.Now()
		resp, err := a.makeRequest(m, d)
		rtt := time.Since(startTime)
		if resp != nil {
			a.updateMessage(resp, p, rtt, err)
			return resp
		}
		a.updateMessage(m, p, rtt, err)
	}
	return m
}

func (a App) updateMessage(m *config.HopMessage, index int64, rtt time.Duration, err error) {
	if index >= int64(len(m.RemoteDests)) {
		m.Error = "no index found for this destination"
		return
	}
	if err != nil {
		m.Error = err.Error()
		return
	}
	if m.RemoteDests[index].Done {
		m.Error = "already went over this destination"
		return
	}
	glog.Infof("Updating message %s at index %d", m.GetId(), index)
	m.RemoteDests[index].Done = true
	m.RemoteDests[index].Rtt = rtt
}
