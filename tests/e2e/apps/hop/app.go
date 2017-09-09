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
	multierror "github.com/hashicorp/go-multierror"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"istio.io/istio/tests/e2e/apps/hop/config"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
)

var (
	timeout     = flag.Duration("timeout", 15*time.Second, "Request timeout")
	version     = flag.String("version", "", "Server version")
	hopYamlTmpl = "tests/e2e/framework/testdata/hop.yam.tmpl"
)

// hopTemplate gathers template variable for hopYamlTmpl.
type hopTemplate struct {
	Deployment string
	Service    string
	HTTPPort   int
	GRPCPort   int
	Version    string
}

// NewHop instantiates a framework.App to be used by framework.AppManager.
func NewHop(d, s, v string, h, g int) *framework.App {
	return &framework.App{
		AppYamlTemplate: util.GetResourcePath(hopYamlTmpl),
		Template: &hopTemplate{
			Deployment: d,
			Service:    s,
			Version:    v,
			HTTPPort:   h,
			GRPCPort:   g,
		},
	}
}

func newHopMessage(u []string) *config.HopMessage {
	r := new(config.HopMessage)
	r.Id = uuid.New().String()
	for _, d := range u {
		dest := new(config.Remote)
		dest.Destination = d
		r.RemoteDests = append(r.RemoteDests, dest)
	}
	glog.Infof("Created Request\n%s", proto.MarshalTextString(r))
	return r
}

// NewApp creates a new Hop App with default settings
func NewApp() *App {
	return newApp(*version)
}

// NewApp creates a new Hop App with default settings
func newApp(version string) *App {
	return &App{
		clientTimeout: *timeout,
		marshaller:    jsonpb.Marshaler{},
		unmarshaller:  jsonpb.Unmarshaler{},
		/* #nosec */
		httpClient: http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
			Timeout: *timeout,
		},
		version: version,
	}
}

// App contains both server and client for Hop App
type App struct {
	marshaller    jsonpb.Marshaler
	unmarshaller  jsonpb.Unmarshaler
	httpClient    http.Client
	clientTimeout time.Duration
	version       string
}

func (a App) makeHTTPRequest(req *config.HopMessage, url string) (*config.HopMessage, error) {
	glog.V(2).Infof("Making HTTP Request to %s", url)
	jsonStr, err := a.marshaller.MarshalToString(req)
	if err != nil {
		return nil, err
	}
	hReq, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer([]byte(jsonStr)))
	if err != nil {
		return nil, err
	}
	hReq.Header.Set("X-Custom-Header", "myvalue")
	hReq.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient.Do(hReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := resp.Body.Close(); e != nil {
			glog.Error(err)
		}
	}()
	var pb config.HopMessage
	if err = a.unmarshaller.Unmarshal(resp.Body, &pb); err != nil {
		return nil, err
	}
	return &pb, err
}

func (a App) makeGRPCRequest(req *config.HopMessage, address string) (*config.HopMessage, error) {
	glog.V(2).Infof("Making GRPC Request to %s", address)
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
	return client.Hop(context.Background(), req)
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
	glog.V(2).Infof("HTTP Serving message %s at position %d", req.GetId(), p)
	resp := a.forwardMessage(req)
	jsonStr, err := a.marshaller.MarshalToString(resp)
	if err != nil {
		glog.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	if _, err = w.Write([]byte(jsonStr)); err != nil {
		glog.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	glog.V(2).Infof("Successfully served message %s at position %d", req.GetId(), p)
}

// Hop start a gRPC server for Hop App
func (a App) Hop(ctx context.Context, req *config.HopMessage) (*config.HopMessage, error) {
	p := req.GetPosition()
	glog.V(2).Infof("GRPC Serving message %s at position %d", req.GetId(), p)
	resp := a.forwardMessage(req)
	glog.V(2).Infof("Successfully served message %s at position %d", req.GetId(), p)
	return resp, nil
}

func (a App) makeRequest(req *config.HopMessage, d string) {
	// Check destination and send using grpc or http handler
	var (
		fn   func(*config.HopMessage, string) (*config.HopMessage, error)
		dest string
	)
	p := req.GetPosition()
	switch {
	case strings.HasPrefix(d, "http://"):
		fn = a.makeHTTPRequest
		dest = d
	case strings.HasPrefix(d, "grpc://"):
		fn = a.makeGRPCRequest
		dest = strings.TrimPrefix(d, "grpc://")
	default:
		err := errors.New("protocol not supported")
		a.clientUpdate(req, p, 0, err)
		return

	}
	startTime := time.Now()
	resp, err := fn(req, dest)
	rtt := time.Since(startTime)
	if resp != nil {
		a.updateMessageFromResponse(req, resp, p)
	}
	a.clientUpdate(req, p, rtt, err)
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
func (a App) MakeRequest(remotes []string) (*config.HopMessage, error) {
	if len(remotes) < 1 {
		return nil, errors.New("remotes can not be an empty slice")
	}
	m := newHopMessage(remotes)
	d := m.GetRemoteDests()[0].GetDestination()
	a.makeRequest(m, d)
	var err error
	for _, r := range m.GetRemoteDests() {
		if r.GetError() != "" {
			err = multierror.Append(err, errors.New(r.GetError()))
		}
	}
	return m, err
}

func (a App) forwardMessage(req *config.HopMessage) *config.HopMessage {
	a.serverUpdate(req)
	a.setNextPosition(req)
	p := req.GetPosition()
	glog.V(2).Infof("Message %s current position is %d", req.GetId(), p)
	if p >= 0 {
		d := req.GetRemoteDests()[p].GetDestination()
		glog.Infof("Forwarding message %s to %s", req.GetId(), d)
		a.makeRequest(req, d)
	}
	return req
}

func (a App) clientUpdate(m *config.HopMessage, index int64, rtt time.Duration, err error) {
	glog.V(2).Infof("Client Update for message %s at index %d", m.GetId(), index)
	m.RemoteDests[index].Done = true
	m.RemoteDests[index].Rtt = rtt
	if err != nil {
		m.RemoteDests[index].Error = err.Error()
	}
}

func (a App) serverUpdate(m *config.HopMessage) {
	index := m.GetPosition()
	glog.V(2).Infof("Server Update for message %s at index %d", m.GetId(), index)
	if a.version != "" {
		m.RemoteDests[index].Version = a.version
	}
}

func (a App) updateMessageFromResponse(m *config.HopMessage, resp *config.HopMessage, index int64) {
	for i := index; i < int64(len(m.GetRemoteDests())); i++ {
		glog.V(2).Infof("Updating message %s from response at index %d", m.GetId(), i)
		*m.RemoteDests[i] = *resp.RemoteDests[i]
	}
}
