// Copyright 2018 Istio Authors
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
package mcp

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"

	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	mcpserver "istio.io/istio/pkg/mcp/server"
)

var fakeCreateTime *types.Timestamp

func init() {
	var err error
	fakeCreateTime, err = types.TimestampProto(time.Date(2018, time.January, 1, 12, 15, 30, 5e8, time.UTC))
	if err != nil {
		panic(err)
	}
}

type mockWatcher struct{}

func (m mockWatcher) Watch(req *mcp.MeshConfigRequest,
	resp chan<- *mcpserver.WatchResponse) (*mcpserver.WatchResponse, mcpserver.CancelWatchFunc) {

	var cancelFunc mcpserver.CancelWatchFunc
	cancelFunc = func() {
		log.Printf("watch canceled for %s\n", req.GetTypeUrl())
	}

	if req.GetTypeUrl() == fmt.Sprintf("type.googleapis.com/%s", model.Gateway.MessageName) {
		marshaledFirstGateway, err := proto.Marshal(firstGateway)
		if err != nil {
			log.Fatalf("marshaling gateway %s\n", err)
		}
		marshaledSecondGateway, err := proto.Marshal(secondGateway)
		if err != nil {
			log.Fatalf("marshaling gateway %s\n", err)
		}

		return &mcpserver.WatchResponse{
			Version: req.GetVersionInfo(),
			TypeURL: req.GetTypeUrl(),
			Envelopes: []*mcp.Envelope{
				{
					Metadata: &mcp.Metadata{
						Name:       "some-name",
						CreateTime: fakeCreateTime,
					},
					Resource: &types.Any{
						TypeUrl: req.GetTypeUrl(),
						Value:   marshaledFirstGateway,
					},
				},
				{
					Metadata: &mcp.Metadata{
						Name:       "some-other-name",
						CreateTime: fakeCreateTime,
					},
					Resource: &types.Any{
						TypeUrl: req.GetTypeUrl(),
						Value:   marshaledSecondGateway,
					},
				},
			},
		}, cancelFunc
	}

	return &mcpserver.WatchResponse{
		Version:   req.GetVersionInfo(),
		TypeURL:   req.GetTypeUrl(),
		Envelopes: []*mcp.Envelope{},
	}, cancelFunc
}

type Server struct {
	// The internal snapshot.Cache that the server is using.
	Watcher *mockWatcher

	// TypeURLs that were originally passed in.
	TypeURLs []string

	// Port that the service is listening on.
	Port int

	// The gRPC compatible address of the service.
	URL *url.URL

	gs *grpc.Server
	l  net.Listener
}

func NewServer(addr string, typeUrls []string) (*Server, error) {
	watcher := mockWatcher{}
	s := mcpserver.New(watcher, typeUrls, mcpserver.NewAllowAllChecker())

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	p := l.Addr().(*net.TCPAddr).Port

	u, err := url.Parse(fmt.Sprintf("tcp://localhost:%d", p))
	if err != nil {
		_ = l.Close()
		return nil, err
	}

	gs := grpc.NewServer()

	mcp.RegisterAggregatedMeshConfigServiceServer(gs, s)
	go func() { _ = gs.Serve(l) }()
	log.Printf("MCP mock server listening on %s", addr)

	return &Server{
		Watcher:  &watcher,
		TypeURLs: typeUrls,
		Port:     p,
		URL:      u,
		gs:       gs,
		l:        l,
	}, nil
}

func (t *Server) Close() (err error) {
	if t.gs != nil {
		t.gs.Stop()
		t.gs = nil
	}

	t.l = nil // gRPC stack will close this
	t.Watcher = nil
	t.TypeURLs = nil
	t.Port = 0

	return
}

var firstGateway = &networking.Gateway{
	Servers: []*networking.Server{
		&networking.Server{
			Port: &networking.Port{
				Name:     "http-8099",
				Number:   8099,
				Protocol: "http",
			},
			Hosts: []string{
				"bar.example.com",
			},
		},
	},
}

var secondGateway = &networking.Gateway{
	Servers: []*networking.Server{
		&networking.Server{
			Port: &networking.Port{
				Name:     "tcp-880",
				Number:   880,
				Protocol: "tcp",
			},
			Hosts: []string{
				"foo.example.org",
			},
		},
	},
}
