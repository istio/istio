// Copyright 2017 Istio Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

// Adapted from istio/proxy/test/backend/echo with error handling and
// concurrency fixes and making it as low overhead as possible
// (no std output by default)

package fgrpc

import (
	"fmt"
	"net"
	"testing"

	"istio.io/fortio/log"
	"istio.io/fortio/periodic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// DynamicGRPCHealthServer starts and returns the port where a GRPC Health
// server is running. It runs until error or program exit (separate go routine)
func DynamicGRPCHealthServer() int {
	socket, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	addr := socket.Addr()
	grpcServer := grpc.NewServer()
	healthServer := health.NewServer()
	healthServer.SetServingStatus("ping", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	fmt.Printf("Fortio %s grpc health server listening on port %v\n", periodic.Version, addr)
	go func(socket net.Listener) {
		if e := grpcServer.Serve(socket); e != nil {
			log.Fatalf("failed to start grpc server: %v", e)
		}
	}(socket)
	return addr.(*net.TCPAddr).Port
}

func TestGRPCRunner(t *testing.T) {
	log.SetLogLevel(log.Info)
	port := DynamicGRPCHealthServer()
	destination := fmt.Sprintf("localhost:%d", port)

	opts := GRPCRunnerOptions{
		RunnerOptions: periodic.RunnerOptions{
			QPS:        100,
			Resolution: 0.00001,
		},
		Destination: destination,
		Profiler:    "test.profile",
	}
	res, err := RunGRPCTest(&opts)
	if err != nil {
		t.Error(err)
		return
	}
	totalReq := res.DurationHistogram.Count
	ok := res.RetCodes[grpc_health_v1.HealthCheckResponse_SERVING]
	if totalReq != ok {
		t.Errorf("Mismatch between requests %d and ok %v", totalReq, res.RetCodes)
	}
}

func TestGRPCDestination(t *testing.T) {
	tests := []struct {
		name   string
		dest   string
		output string
	}{
		{
			"hostname",
			"localhost",
			"localhost:8079",
		},
		{
			"hostname and port",
			"localhost:1234",
			"localhost:1234",
		},
		{
			"IPv4 address",
			"1.2.3.4",
			"1.2.3.4:8079",
		},
		{
			"IPv4 address and port",
			"1.2.3.4:5678",
			"1.2.3.4:5678",
		},
		{
			"IPv6 address",
			"2001:dba::1",
			"[2001:dba::1]:8079",
		},
		{
			"IPv6 address and port",
			"[2001:dba::1]:1234",
			"[2001:dba::1]:1234",
		},
		{
			"hostname and no port",
			"foo.bar.com",
			"foo.bar.com:8079",
		},
		{
			"hostname and port",
			"foo.bar.com:123",
			"foo.bar.com:123",
		},
	}

	for _, tc := range tests {
		dest := GRPCDestination(tc.dest)
		if dest != tc.output {
			t.Errorf("Test case %s failed to set gRPC destination\n\texpected: %s\n\t  actual: %s",
				tc.name,
				tc.output,
				dest,
			)
		}
	}
}
