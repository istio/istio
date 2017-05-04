// Copyright 2017 Google Inc.
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
// limitations under the License.package hop

package hop

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"istio.io/istio/tests/e2e/apps/hop/config"
)

const seed int64 = 1823543

type testServers struct {
	gs      []*grpc.Server
	hs      []*httptest.Server
	remotes []string
}

type errorServer struct{}

type handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Hop(ctx context.Context, req *config.HopMessage) (*config.HopMessage, error)
}

// ServerHTTP implements the Hop HTTP server.
func (a errorServer) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "random error", http.StatusInternalServerError)
}

// Hop implements the Hop gRPC server.
func (a errorServer) Hop(_ context.Context, _ *config.HopMessage) (*config.HopMessage, error) {
	return nil, errors.New("random error")
}

func newTestServers(grpcCount, httpCount, grpcErrorCount, httpErrorCount int) (*testServers, error) {
	ts := testServers{
		gs:      []*grpc.Server{},
		hs:      []*httptest.Server{},
		remotes: []string{},
	}
	for i := 0; i < grpcCount; i++ {
		v := fmt.Sprintf("g%d", i)
		s, address, err := startGRPCServer(newApp(v), v)
		if err != nil {
			ts.shutdown()
			return nil, err
		}
		ts.gs = append(ts.gs, s)
		ts.remotes = append(ts.remotes, fmt.Sprintf("grpc://%s", address))
	}
	for i := 0; i < grpcErrorCount; i++ {
		s, address, err := startGRPCServer(&errorServer{}, fmt.Sprintf("eg%d", i))
		if err != nil {
			ts.shutdown()
			return nil, err
		}
		ts.gs = append(ts.gs, s)
		ts.remotes = append(ts.remotes, fmt.Sprintf("grpc://%s", address))
	}
	for i := 0; i < httpCount; i++ {
		v := fmt.Sprintf("h%d", i)
		s := startHTTPServer(newApp(v), v)
		ts.hs = append(ts.hs, s)
		ts.remotes = append(ts.remotes, s.URL)
	}
	for i := 0; i < httpErrorCount; i++ {
		s := startHTTPServer(&errorServer{}, fmt.Sprintf("eh%d", i))
		ts.hs = append(ts.hs, s)
		ts.remotes = append(ts.remotes, s.URL)
	}
	return &ts, nil
}

func (ts *testServers) shutdown() {
	for _, s := range ts.hs {
		s.Close()
	}
	for _, s := range ts.gs {
		s.GracefulStop()
	}
}

func (ts *testServers) shuffledRemotes() []string {
	rand.Seed(seed)
	dest := make([]string, len(ts.remotes))
	for i, v := range rand.Perm(len(ts.remotes)) {
		dest[i] = (ts.remotes)[v]
	}
	return dest
}

func TestMultipleHTTPSuccess(t *testing.T) {
	a := NewApp()
	ts, err := newTestServers(0, 3, 0, 0)
	if err != nil {
		t.Error(err)
	}
	defer ts.shutdown()
	remotes := ts.shuffledRemotes()
	resp, err := a.MakeRequest(remotes)
	if err != nil {
		t.Error(err)
	}
	if resp == nil {
		t.Errorf("response cannot be nit")
	}
	for _, r := range resp.GetRemoteDests() {
		if !r.GetDone() {
			t.Errorf("%s has not been visited", r.GetDestination())
		}
	}
	t.Logf("Response is \n%s", proto.MarshalTextString(resp))
}

func TestMultipleGRPCSuccess(t *testing.T) {
	a := NewApp()
	ts, err := newTestServers(3, 0, 0, 0)
	if err != nil {
		t.Error(err)
	}
	defer ts.shutdown()
	remotes := ts.shuffledRemotes()
	resp, err := a.MakeRequest(remotes)
	if err != nil {
		t.Error(err)
	}
	if resp == nil {
		t.Errorf("resp cannot be nil")
	}
	for _, r := range resp.GetRemoteDests() {
		if !r.GetDone() {
			t.Errorf("%s has not been visited", r.GetDestination())
		}
	}
	t.Logf("Response is \n%s", proto.MarshalTextString(resp))
}

func TestRandomSuccess(t *testing.T) {
	a := NewApp()
	ts, err := newTestServers(2, 2, 0, 0)
	if err != nil {
		t.Error(err)
	}
	defer ts.shutdown()
	resp, err := a.MakeRequest(ts.shuffledRemotes())
	if err != nil {
		t.Error(err)
	}
	if resp == nil {
		t.Errorf("response cannot be nil")
	}
	for _, r := range resp.GetRemoteDests() {
		if !r.GetDone() {
			t.Errorf("%s has not been visited", r.GetDestination())
		}
	}
	t.Logf("Response is \n%s", proto.MarshalTextString(resp))
}

func TestHTTPRandomFailure(t *testing.T) {
	a := NewApp()
	ts, err := newTestServers(0, 2, 0, 0)
	if err != nil {
		t.Error(err)
	}
	defer ts.shutdown()
	tsErr, err := newTestServers(0, 0, 0, 1)
	if err != nil {
		t.Error(err)
	}
	defer tsErr.shutdown()
	remotes := append(ts.shuffledRemotes(), tsErr.remotes...)
	resp, err := a.MakeRequest(remotes)
	if err == nil {
		t.Errorf("should have failed")
	}
	if resp == nil {
		t.Errorf("resp cannot be nil")
	}
	// Going thru all the nodes but the last one, as we did not visist the last one.
	for i := 0; i < len(remotes)-1; i++ {
		r := resp.GetRemoteDests()[i]
		if !r.GetDone() {
			t.Errorf("%s has not been visited", r.GetDestination())
		}
	}
	t.Logf("Response is \n%s", proto.MarshalTextString(resp))
}

func TestGRPCRandomFailure(t *testing.T) {
	a := NewApp()
	ts, err := newTestServers(2, 0, 0, 0)
	if err != nil {
		t.Error(err)
	}
	defer ts.shutdown()
	tsErr, err := newTestServers(0, 0, 1, 0)
	if err != nil {
		t.Error(err)
	}
	defer tsErr.shutdown()
	remotes := append(ts.shuffledRemotes(), tsErr.remotes...)
	resp, err := a.MakeRequest(remotes)
	if err == nil {
		t.Errorf("should have failed")
	}
	if resp == nil {
		t.Errorf("resp cannot be nil")
	}
	// Going thru all the nodes but the last one, as we did not visist the last one.
	for i := 0; i < len(remotes)-1; i++ {
		r := resp.GetRemoteDests()[i]
		if !r.GetDone() {
			t.Errorf("%s has not been visited", r.GetDestination())
		}
	}
	t.Logf("Response is \n%s", proto.MarshalTextString(resp))
}

func TestRandomFailure(t *testing.T) {
	a := NewApp()
	ts, err := newTestServers(2, 2, 0, 0)
	if err != nil {
		t.Error(err)
	}
	defer ts.shutdown()
	tsErr, err := newTestServers(0, 0, 1, 1)
	if err != nil {
		t.Error(err)
	}
	defer tsErr.shutdown()
	remotes := append(ts.shuffledRemotes(), tsErr.shuffledRemotes()...)
	resp, err := a.MakeRequest(remotes)
	if err == nil {
		t.Errorf("should have failed")
	}
	if resp == nil {
		t.Errorf("resp cannot be nil")
	}
	if resp.GetRemoteDests()[len(remotes)-1].GetDone() {
		t.Errorf("last node should be be visited")
	}
	// Going thru all the nodes but the last one, as we did not visist the last one.
	for i := 0; i < len(remotes)-1; i++ {
		r := resp.GetRemoteDests()[i]
		if !r.GetDone() {
			t.Errorf("%s has not been visited", r.GetDestination())
		}
	}
	t.Logf("Response is \n%s", proto.MarshalTextString(resp))
}

func startHTTPServer(a handler, n string) *httptest.Server {
	s := httptest.NewServer(a)
	glog.Infof("Server %s started at %s", n, s.URL)
	return s
}

func startGRPCServer(a handler, n string) (*grpc.Server, string, error) {
	glog.Info("Starting GRPC server")
	s := grpc.NewServer()
	config.RegisterHopTestServiceServer(s, a)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", 0))
	if err != nil {
		return nil, "", err
	}
	go s.Serve(lis) // nolint: errcheck
	address := lis.Addr().String()
	glog.Infof("Server %s started at %s", n, address)
	return s, address, nil
}
