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

package fgrpc

import (
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"fortio.org/fortio/fnet"
	"fortio.org/fortio/log"
	"fortio.org/fortio/stats"
)

const (
	// DefaultHealthServiceName is the default health service name used by fortio.
	DefaultHealthServiceName = "ping"
	//Error indicates that something went wrong with healthcheck in grpc
	Error = "ERROR"
)

type pingSrv struct {
}

func (s *pingSrv) Ping(c context.Context, in *PingMessage) (*PingMessage, error) {
	log.LogVf("Ping called %+v (ctx %+v)", *in, c)
	out := *in // copy the input including the payload etc
	out.Ts = time.Now().UnixNano()
	if in.DelayNanos > 0 {
		s := time.Duration(in.DelayNanos)
		log.LogVf("GRPC ping: sleeping for %v", s)
		time.Sleep(s)
	}
	return &out, nil
}

// PingServer starts a grpc ping (and health) echo server.
// returns the port being bound (useful when passing "0" as the port to
// get a dynamic server). Pass the healthServiceName to use for the
// grpc service name health check (or pass DefaultHealthServiceName)
// to be marked as SERVING. Pass maxConcurrentStreams > 0 to set that option.
func PingServer(port, cert, key, healthServiceName string, maxConcurrentStreams uint32) net.Addr {
	socket, addr := fnet.Listen("grpc '"+healthServiceName+"'", port)
	if addr == nil {
		return nil
	}
	var grpcOptions []grpc.ServerOption
	if maxConcurrentStreams > 0 {
		log.Infof("Setting grpc.MaxConcurrentStreams server to %d", maxConcurrentStreams)
		grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(maxConcurrentStreams))
	}
	if cert != "" && key != "" {
		creds, err := credentials.NewServerTLSFromFile(cert, key)
		if err != nil {
			log.Fatalf("Invalid TLS credentials: %v\n", err)
		}
		log.Infof("Using server certificate %v to construct TLS credentials", cert)
		log.Infof("Using server key %v to construct TLS credentials", key)
		grpcOptions = append(grpcOptions, grpc.Creds(creds))
	}
	grpcServer := grpc.NewServer(grpcOptions...)
	reflection.Register(grpcServer)
	healthServer := health.NewServer()
	healthServer.SetServingStatus(healthServiceName, grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(grpcServer, healthServer)
	RegisterPingServerServer(grpcServer, &pingSrv{})
	go func() {
		if err := grpcServer.Serve(socket); err != nil {
			log.Fatalf("failed to start grpc server: %v", err)
		}
	}()
	return addr
}

// PingServerTCP is PingServer() assuming tcp instead of possible unix domain socket port, returns
// the numeric port.
func PingServerTCP(port, cert, key, healthServiceName string, maxConcurrentStreams uint32) int {
	addr := PingServer(port, cert, key, healthServiceName, maxConcurrentStreams)
	if addr == nil {
		return -1
	}
	return addr.(*net.TCPAddr).Port
}

// PingClientCall calls the ping service (presumably running as PingServer on
// the destination). returns the average round trip in seconds.
func PingClientCall(serverAddr, cacert string, n int, payload string, delay time.Duration) (float64, error) {
	o := GRPCRunnerOptions{Destination: serverAddr, CACert: cacert}
	conn, err := Dial(&o) // somehow this never seem to error out, error comes later
	if err != nil {
		return -1, err // error already logged
	}
	msg := &PingMessage{Payload: payload, DelayNanos: delay.Nanoseconds()}
	cli := NewPingServerClient(conn)
	// Warm up:
	_, err = cli.Ping(context.Background(), msg)
	if err != nil {
		log.Errf("grpc error from Ping0 %v", err)
		return -1, err
	}
	skewHistogram := stats.NewHistogram(-10, 2)
	rttHistogram := stats.NewHistogram(0, 10)
	for i := 1; i <= n; i++ {
		msg.Seq = int64(i)
		t1a := time.Now().UnixNano()
		msg.Ts = t1a
		res1, err := cli.Ping(context.Background(), msg)
		t2a := time.Now().UnixNano()
		if err != nil {
			log.Errf("grpc error from Ping1 iter %d: %v", i, err)
			return -1, err
		}
		t1b := res1.Ts
		res2, err := cli.Ping(context.Background(), msg)
		t3a := time.Now().UnixNano()
		t2b := res2.Ts
		if err != nil {
			log.Errf("grpc error from Ping2 iter %d: %v", i, err)
			return -1, err
		}
		rt1 := t2a - t1a
		rttHistogram.Record(float64(rt1) / 1000.)
		rt2 := t3a - t2a
		rttHistogram.Record(float64(rt2) / 1000.)
		rtR := t2b - t1b
		rttHistogram.Record(float64(rtR) / 1000.)
		midR := t1b + (rtR / 2)
		avgRtt := (rt1 + rt2 + rtR) / 3
		x := (midR - t2a)
		log.Infof("Ping RTT %d (avg of %d, %d, %d ns) clock skew %d",
			avgRtt, rt1, rtR, rt2, x)
		skewHistogram.Record(float64(x) / 1000.)
		msg = res2
	}
	skewHistogram.Print(os.Stdout, "Clock skew histogram usec", []float64{50})
	rttHistogram.Print(os.Stdout, "RTT histogram usec", []float64{50})
	return rttHistogram.Avg() / 1e6, nil
}

// HealthResultMap short cut for the map of results to count.
type HealthResultMap map[string]int64

// GrpcHealthCheck makes a grpc client call to the standard grpc health check
// service.
func GrpcHealthCheck(serverAddr, cacert string, svcname string, n int) (*HealthResultMap, error) {
	log.Debugf("GrpcHealthCheck for %s svc '%s', %d iterations", serverAddr, svcname, n)
	o := GRPCRunnerOptions{Destination: serverAddr, CACert: cacert}
	conn, err := Dial(&o)
	if err != nil {
		return nil, err
	}
	msg := &grpc_health_v1.HealthCheckRequest{Service: svcname}
	cli := grpc_health_v1.NewHealthClient(conn)
	rttHistogram := stats.NewHistogram(0, 10)
	statuses := make(HealthResultMap)

	for i := 1; i <= n; i++ {
		start := time.Now()
		res, err := cli.Check(context.Background(), msg)
		dur := time.Since(start)
		log.LogVf("Reply from health check %d: %+v", i, res)
		if err != nil {
			log.Errf("grpc error from Check %v", err)
			return nil, err
		}
		statuses[res.Status.String()]++
		rttHistogram.Record(dur.Seconds() * 1000000.)
	}
	rttHistogram.Print(os.Stdout, "RTT histogram usec", []float64{50})
	for k, v := range statuses {
		fmt.Printf("Health %s : %d\n", k, v)
	}
	return &statuses, nil
}
