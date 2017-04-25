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
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"istio.io/istio/tests/e2e/apps/hop/config"
)

type testServers struct {
	gs      *[]*grpc.Server
	hs      *[]*httptest.Server
	remotes *[]string
}

type errorServer struct{}

type handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Hop(ctx context.Context, req *config.HopMessage) (*config.HopMessage, error)
}

func logError(t *testing.T, err error) {
	glog.Error(err)
	t.Error(err)
}

func (a errorServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "random error", http.StatusInternalServerError)
}

// Hop start a gRPC server for Hop App
func (a errorServer) Hop(ctx context.Context, req *config.HopMessage) (*config.HopMessage, error) {
	return nil, errors.New("random error")
}

func newTestServers(grpcCount int, httpCount int, grpcErrorCount int, httpErrorCount int) (*testServers, error) {
	gs := []*grpc.Server{}
	hs := []*httptest.Server{}
	remotes := []string{}
	t := testServers{
		gs:      &gs,
		hs:      &hs,
		remotes: &remotes,
	}
	for i := 0; i < grpcCount; i++ {
		s, address, err := startGRPCServer(NewApp(), fmt.Sprintf("h%d", i))
		gs = append(gs, s)
		remotes = append(remotes, fmt.Sprintf("grpc://%s", address))
		if err != nil {
			t.shutdown()
			return nil, err
		}
	}
	for i := 0; i < grpcErrorCount; i++ {
		s, address, err := startGRPCServer(&errorServer{}, fmt.Sprintf("h%d", i))
		gs = append(gs, s)
		remotes = append(remotes, fmt.Sprintf("grpc://%s", address))
		if err != nil {
			t.shutdown()
			return nil, err
		}
	}
	for i := 0; i < httpCount; i++ {
		s := startHTTPServer(NewApp(), fmt.Sprintf("h%d", i))
		hs = append(hs, s)
		remotes = append(remotes, s.URL)
	}
	for i := 0; i < httpErrorCount; i++ {
		s := startHTTPServer(&errorServer{}, fmt.Sprintf("h%d", i))
		hs = append(hs, s)
		remotes = append(remotes, s.URL)
	}
	return &t, nil
}

func (ss *testServers) shutdown() {
	for _, s := range *ss.hs {
		s.Close()
	}
	for _, s := range *ss.gs {
		s.GracefulStop()
	}
}

func (ss *testServers) shuffledRemotes() *[]string {
	rand.Seed(time.Now().UnixNano())
	dest := make([]string, len(*ss.remotes))
	for i, v := range rand.Perm(len(*ss.remotes)) {
		dest[i] = (*ss.remotes)[v]
	}
	return &dest
}

func TestMultipleHTTPSuccess(t *testing.T) {
	a := NewApp()
	ss, err := newTestServers(0, 3, 0, 0)
	if err != nil {
		glog.Error(err)
		t.Error(err)
	}
	defer ss.shutdown()
	remotes := ss.shuffledRemotes()
	resp, err := a.MakeRequest(remotes)
	if err != nil {
		logError(t, err)
	}
	if resp == nil {
		logError(t, errors.New("response cannot be nit"))
	}
	for _, r := range resp.GetRemoteDests() {
		if !r.GetDone() {
			logError(t, fmt.Errorf("%s has not been visited", r.GetDestination()))
		}
	}
	glog.Infof("Response is \n%s", proto.MarshalTextString(resp))
}

func TestMultipleGRPCSuccess(t *testing.T) {
	a := NewApp()
	ss, err := newTestServers(3, 0, 0, 0)
	if err != nil {
		logError(t, err)
	}
	defer ss.shutdown()
	remotes := ss.shuffledRemotes()
	resp, err := a.MakeRequest(remotes)
	if err != nil {
		logError(t, err)
	}
	if resp == nil {
		logError(t, errors.New("resp cannot be nil"))
	}
	for _, r := range resp.GetRemoteDests() {
		if !r.GetDone() {
			logError(t, fmt.Errorf("%s has not been visited", r.GetDestination()))
		}
	}
	glog.Infof("Response is \n%s", proto.MarshalTextString(resp))
}

func TestRandomSuccess(t *testing.T) {
	a := NewApp()
	ss, err := newTestServers(2, 2, 0, 0)
	if err != nil {
		glog.Error(err)
		t.Error(err)
	}
	defer ss.shutdown()
	resp, err := a.MakeRequest(ss.shuffledRemotes())
	if err != nil {
		logError(t, err)
	}
	if resp == nil {
		logError(t, errors.New("response cannot be nil"))
	}
	for _, r := range resp.GetRemoteDests() {
		if !r.GetDone() {
			logError(t, fmt.Errorf("%s has not been visited", r.GetDestination()))
		}
	}
	glog.Infof("Response is \n%s", proto.MarshalTextString(resp))
}

func TestHTTPRandomFailure(t *testing.T) {
	a := NewApp()
	ss, err := newTestServers(0, 2, 0, 0)
	if err != nil {
		logError(t, err)
	}
	defer ss.shutdown()
	sserr, err := newTestServers(0, 0, 0, 1)
	if err != nil {
		logError(t, err)
	}
	defer sserr.shutdown()
	remotes := append(*ss.shuffledRemotes(), *sserr.remotes...)
	resp, err := a.MakeRequest(&remotes)
	if err == nil {
		logError(t, errors.New("should have failed"))
	}
	if resp == nil {
		logError(t, errors.New("resp cannot be nil"))
	}
	for i := 0; i < len(remotes)-1; i++ {
		r := resp.GetRemoteDests()[i]
		if !r.GetDone() {
			logError(t, fmt.Errorf("%s has not been visited", r.GetDestination()))
		}
	}
	glog.Infof("Response is \n%s", proto.MarshalTextString(resp))
}

func TestGRPCRandomFailure(t *testing.T) {
	a := NewApp()
	ss, err := newTestServers(2, 0, 0, 0)
	if err != nil {
		logError(t, err)
	}
	defer ss.shutdown()
	sserr, err := newTestServers(0, 0, 1, 0)
	if err != nil {
		logError(t, err)
	}
	defer sserr.shutdown()
	remotes := append(*ss.shuffledRemotes(), *sserr.remotes...)
	resp, err := a.MakeRequest(&remotes)
	if err == nil {
		logError(t, errors.New("should have failed"))
	}
	if resp == nil {
		logError(t, errors.New("resp cannot be nil"))
	}
	for i := 0; i < len(remotes)-1; i++ {
		r := resp.GetRemoteDests()[i]
		if !r.GetDone() {
			logError(t, fmt.Errorf("%s has not been visited", r.GetDestination()))
		}
	}
	glog.Infof("Response is \n%s", proto.MarshalTextString(resp))
}

func TestRandomFailure(t *testing.T) {
	a := NewApp()
	ss, err := newTestServers(2, 2, 0, 0)
	if err != nil {
		logError(t, err)
	}
	defer ss.shutdown()
	sserr, err := newTestServers(0, 0, 1, 1)
	if err != nil {
		logError(t, err)
	}
	defer sserr.shutdown()
	remotes := append(*ss.shuffledRemotes(), *sserr.shuffledRemotes()...)
	resp, err := a.MakeRequest(&remotes)
	if err == nil {
		logError(t, errors.New("should have failed"))
	}
	if resp == nil {
		logError(t, errors.New("resp cannot be nil"))
	}
	for i := 0; i < len(remotes)-1; i++ {
		r := resp.GetRemoteDests()[i]
		if !r.GetDone() {
			logError(t, fmt.Errorf("%s has not been visited", r.GetDestination()))
		}
	}
	glog.Infof("Response is \n%s", proto.MarshalTextString(resp))
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
	go func() {
		_ = s.Serve(lis)
	}()
	address := lis.Addr().String()
	glog.Infof("Server %s started at %s", n, address)
	return s, address, nil
}
