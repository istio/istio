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
	"net/http"
	"strings"
	"time"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
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
	glog.Infof("Created Request\n%s", proto.MarshalTextString(r))
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

func (a App) makeHTTPRequest(req *config.HopMessage, url string) (*config.HopMessage, error) {
	glog.Infof("Making HTTP Request to %s", url)
	jsonStr, err := a.marshaller.MarshalToString(req)
	if err != nil {
		return nil, err
	}
	hreq, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(jsonStr)))
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("X-Custom-Header", "myvalue")
	hreq.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(hreq)
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
		if e := conn.Close(); e != nil {
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
	p := req.GetPosition()
	glog.Infof("GRPC Serving message %s at position %d", req.GetId(), p)
	resp := a.forwardMessage(req)
	glog.Infof("Successfully served message %s at position %d", req.GetId(), p)
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
	p := m.GetPosition()
	if p < 0 {
		return
	}
	if m.GetRemoteDests()[p].GetError() != "" {
		// Error along the way no need to continue
		m.Position = -1
		return
	}
	nextPos := m.GetPosition() + 1
	if nextPos < int64(len(m.GetRemoteDests())) {
		m.Position = nextPos
		return
	}
	m.Position = -1
}

// MakeRequest will create a config.HopMessage proto and
// send requests to defined remotes hosts in a chain.
// Each Server is a client and will forward the call to the next hop.
func (a App) MakeRequest(remotes *[]string) (*config.HopMessage, error) {
	req := newHopMessage(remotes)
	resp := a.forwardMessage(req)
	var err error
	for _, r := range resp.GetRemoteDests() {
		if r.GetError() != "" {
			err = errors.New(r.GetError())
		}
	}
	return resp, err
}

func (a App) forwardMessage(m *config.HopMessage) *config.HopMessage {
	p := m.GetPosition()
	glog.Infof("Message %s current position is %d", m.GetId(), p)
	if p >= 0 {
		a.setNextPosition(m)
		d := m.GetRemoteDests()[p].GetDestination()
		glog.Infof("Forwarding message %s to %s", m.GetId(), d)
		startTime := time.Now()
		resp, err := a.makeRequest(m, d)
		rtt := time.Since(startTime)
		if resp != nil {
			a.updateMessageFromResponse(m, resp, p+1)
		}
		a.updateMessage(m, p, rtt, err)
		return m
	}
	return m
}

func (a App) updateMessage(m *config.HopMessage, index int64, rtt time.Duration, err error) {
	glog.Infof("Updating message %s at index %d", m.GetId(), index)
	m.RemoteDests[index].Done = true
	m.RemoteDests[index].Rtt = rtt
	if err != nil {
		m.RemoteDests[index].Error = err.Error()
	}
}

func (a App) updateMessageFromResponse(m *config.HopMessage, resp *config.HopMessage, index int64) {
	for i := index; i < int64(len(m.GetRemoteDests())); i++ {
		if !resp.GetRemoteDests()[i].GetDone() {
			break
		}
		glog.Infof("Updating message from response %s at index %d", m.GetId(), i)
		*m.RemoteDests[i] = *resp.RemoteDests[i]
	}
}
