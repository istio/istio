// Copyright 2020 Istio Authors
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

package mock

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	listener "github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	route "github.com/envoyproxy/go-control-plane/envoy/api/v2/route"
	hcm "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	xds "github.com/envoyproxy/go-control-plane/pkg/server"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	// credentialTokenHeaderKey is the header key in gPRC header which is used to
	// pass credential token from envoy's SDS request to SDS service.
	credentialTokenHeaderKey = "authorization"
)

type DynamicListener struct {
	Port int
}

func (l *DynamicListener) makeListener() *api.Listener {
	manager := &hcm.HttpConnectionManager{
		CodecType:  hcm.HttpConnectionManager_AUTO,
		StatPrefix: "http",
		RouteSpecifier: &hcm.HttpConnectionManager_RouteConfig{
			RouteConfig: &api.RouteConfiguration{
				Name: "testListener",
				VirtualHosts: []*route.VirtualHost{{
					Name:    "backend",
					Domains: []string{"*"},
					Routes: []*route.Route{{
						Match: &route.RouteMatch{PathSpecifier: &route.RouteMatch_Prefix{Prefix: "/"}},
						Action: &route.Route_Route{Route: &route.RouteAction{
							ClusterSpecifier: &route.RouteAction_Cluster{Cluster: "backend"},
						}},
					}}}}}},
		HttpFilters: []*hcm.HttpFilter{{
			Name: wellknown.Router,
		}},
	}

	hTTPConnectionManager, err := conversion.MessageToStruct(manager)
	if err != nil {
		panic(err)
	}

	return &api.Listener{
		Name: strconv.Itoa(l.Port),
		Address: &core.Address{Address: &core.Address_SocketAddress{SocketAddress: &core.SocketAddress{
			Address:       "127.0.0.1",
			PortSpecifier: &core.SocketAddress_PortValue{PortValue: uint32(l.Port)}}}},
		FilterChains: []*listener.FilterChain{{
			Filters: []*listener.Filter{{
				Name:       wellknown.HTTPConnectionManager,
				ConfigType: &listener.Filter_Config{Config: hTTPConnectionManager},
			}},
		}},
	}
}

type hasher struct{}

func (hasher) ID(*core.Node) string {
	return ""
}

// StartXDSServer sets up a mock XDS server
func StartXDSServer(t *testing.T, xdsPort int, cb *XDSCallbacks, ls *DynamicListener) *grpc.Server {
	snapshotCache := cache.NewSnapshotCache(false, hasher{}, nil)
	server := xds.NewServer(context.Background(), snapshotCache, cb)
	gRPCServer := grpc.NewServer()

	lis, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", xdsPort))
	if err != nil {
		t.Fatalf("xDS server failed to listen on %s: %v", fmt.Sprintf(":%d", xdsPort), err)
	}
	t.Logf("xDS server listens on %s", lis.Addr().String())
	discovery.RegisterAggregatedDiscoveryServiceServer(gRPCServer, server)
	snapshotCache.SetSnapshot("", cache.Snapshot{
		Listeners: cache.Resources{Version: time.Now().String(), Items: map[string]cache.Resource{
			"backend": ls.makeListener()}}})
	go func() {
		_ = gRPCServer.Serve(lis)
	}()
	return gRPCServer
}

type XDSCallbacks struct {
	numStream         int
	numTokenReceived  int
	callbackError     bool
	lastReceivedToken string
	mutex             sync.RWMutex

	t *testing.T
}

func CreateXdsCallback(t *testing.T) *XDSCallbacks {
	return &XDSCallbacks{t: t}
}

func (c *XDSCallbacks) SetCallbackError(setErr bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.callbackError = setErr
}

func (c *XDSCallbacks) NumStream() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.numStream
}

func (c *XDSCallbacks) NumTokenReceived() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.numTokenReceived
}

func (c *XDSCallbacks) OnStreamOpen(ctx context.Context, id int64, url string) error {
	c.t.Logf("xDS stream (id: %d, url: %s) is open", id, url)

	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.numStream++
	if metadata, ok := metadata.FromIncomingContext(ctx); ok {
		if h, ok := metadata[credentialTokenHeaderKey]; ok {
			if len(h) != 1 {
				c.t.Logf("xDS stream (id: %d, url: %s) sends multiple tokens (%d)", id, url, len(h))
			}
			if h[0] != c.lastReceivedToken {
				c.numTokenReceived++
				c.lastReceivedToken = h[0]
			}
		} else {
			c.t.Errorf("XDS stream (id: %d, url: %s) does not have token in metadata %+v",
				id, url, metadata)
		}
	} else {
		c.t.Errorf("failed to get metadata from XDS stream (id: %d, url: %s)", id, url)
	}

	if c.callbackError {
		return errors.New("fake stream error")
	}
	return nil
}
func (c *XDSCallbacks) OnStreamClosed(id int64) {
	c.t.Logf("xDS stream (id: %d) is closed", id)
}
func (c *XDSCallbacks) OnStreamRequest(id int64, _ *api.DiscoveryRequest) error {
	c.t.Logf("receive xDS request (id: %d)", id)
	return nil
}
func (c *XDSCallbacks) OnStreamResponse(id int64, _ *api.DiscoveryRequest, _ *api.DiscoveryResponse) {
	c.t.Logf("on stream %d response", id)
}
func (c *XDSCallbacks) OnFetchRequest(context.Context, *api.DiscoveryRequest) error {
	c.t.Logf("on fetch request")
	return nil
}
func (c *XDSCallbacks) OnFetchResponse(*api.DiscoveryRequest, *api.DiscoveryResponse) {
	c.t.Logf("on fetch response")
}
