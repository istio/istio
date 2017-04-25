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
	"fmt"
	"net"
	"net/http/httptest"
	"testing"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc"

	"istio.io/istio/tests/e2e/apps/hop/config"
)

func TestMultipleHTTPAppSuccess(t *testing.T) {
	a := NewApp()
	hs := []httptest.Server{}
	remotes := []string{}
	for i := 0; i < 3; i++ {
		s := startHTTPServer(a, fmt.Sprintf("h%d", i))
		hs = append(hs, *s)
		remotes = append(remotes, s.URL)
	}
	defer func() {
		for _, s := range hs {
			s.Close()
		}
	}()
	resp, err := a.MakeRequest(&remotes)
	if err != nil {
		glog.Errorf("Error is %s", err)
		t.Error(err)
	}
	if resp == nil {
		t.Errorf("response cannot be nil")
	}
	glog.Infof("Response is \n%s", proto.MarshalTextString(resp))
}

func TestSingleGRPCAppSuccess(t *testing.T) {
	a := NewApp()
	hs := []grpc.Server{}
	remotes := []string{}
	for i := 0; i < 3; i++ {
		s, address, err := startGRPCServer(a, fmt.Sprintf("h%d", i))
		hs = append(hs, *s)
		remotes = append(remotes, fmt.Sprintf("grpc://%s", address))
		if err != nil {
			glog.Error(err)
			t.Error(err)
		}
	}
	defer func() {
		for _, s := range hs {
			s.GracefulStop()

		}
	}()
	resp, err := a.MakeRequest(&remotes)
	if err != nil {
		glog.Errorf("Error is %s", err)
		t.Error(err)
	}
	if resp == nil {
		t.Errorf("response cannot be nil")
	}
	glog.Infof("Response is \n%s", proto.MarshalTextString(resp))
}

func startHTTPServer(a *App, n string) *httptest.Server {
	s := httptest.NewServer(a)
	glog.Infof("Server %s started at %s", n, s.URL)
	return s
}

func startGRPCServer(a *App, n string) (*grpc.Server, string, error) {
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
