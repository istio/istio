// Copyright 2018 Istio Authors
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.

// +build ignore

// TODO: This is a placeholder file to import go-control-plane. This file
// will be removed and substituted with real code.

package v2

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/envoyproxy/go-control-plane/api"
	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/server"
	"google.golang.org/grpc"

	"istio.io/istio/pkg/log"
)

// RunADS starts an ADS server at the given port.
func RunXDS(ctx context.Context, config cache.Cache, port uint) {
	server := xds.NewServer(config)
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Errorf("failed to listen")
	}
	api.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	log.Infof("xDS server listening on %d", port)
	go func() {
		if err = grpcServer.Serve(lis); err != nil {
			log.Errorf("Failed to start gRPC server")
		}
	}()
	<-ctx.Done()
	grpcServer.GracefulStop()
}

// DataPlaneConfigRefresh periodically refreshes configuration of each node
// in the data plane.  Platform layer updates the cache/map of endpoints,
// labels, services, etc. on demand.  Every x seconds, data plane cache
// updater sweeps through the cache, recomputes (if need be) configs per
// node and updates the data plane cache snapshots. Updates to data plane
// cache snapshots causes new config to be pushed to envoy.
// Clusters, listeners and routes are reused across different snapshots.
func DataPlaneConfigRefresh(ctx context.Context, config cache.Cache, interval time.Duration) {
	for {
		// for each node in the data plane api cache
		// compute node-level config if platform info has changed
		// call config.SetSnapshot which will push new version to envoy
		// snapshot := cache.NewSnapshot(version,
		// 	[]proto.Message{endpoint},
		// 	[]proto.Message{cluster},
		// 	[]proto.Message{route},
		// 	[]proto.Message{listener})
		// config.SetSnapshot(cache.Key(node), snapshot)

		select {
		case <-time.After(interval):
		case <-ctx.Done():
			return
		}
	}
}
