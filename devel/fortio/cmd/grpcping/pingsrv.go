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

package main

import (
	"flag"
	"fmt"
	"net"
	"time"

	"istio.io/istio/devel/fortio"

	context "golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var (
	countFlag   = flag.Int("n", 1, "how many ping the client will send")
	portFlag    = flag.Int("port", 8079, "default grpc port")
	hostFlag    = flag.String("host", "", "client mode: server to connect to")
	payloadFlag = flag.String("payload", "this is the default payload", "Payload string to send along")
)

type pingServer struct {
}

func (s *pingServer) Ping(c context.Context, in *PingMessage) (*PingMessage, error) {
	fortio.LogVf("Ping called %+v (ctx %+v)", *in, c)
	out := *in
	out.Ts = time.Now().UnixNano()
	return &out, nil
}

func startServer(port int) {
	socket, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fortio.Fatalf("failed to listen: %v", err)
	}
	grpcServer := grpc.NewServer()
	reflection.Register(grpcServer)
	RegisterPingServerServer(grpcServer, &pingServer{})
	fmt.Printf("Fortio %s grpc ping server listening on port %v\n", fortio.Version, port)
	if err := grpcServer.Serve(socket); err != nil {
		fortio.Fatalf("failed to start grpc server: %v", err)
	}
}

func clientCall(serverAddr string, n int, payload string) {
	conn, err := grpc.Dial(serverAddr, grpc.WithInsecure())
	if err != nil {
		fortio.Fatalf("failed to conect to %s: %v", serverAddr, err)
	}
	msg := &PingMessage{}
	msg.Payload = payload
	cli := NewPingServerClient(conn)
	// Warm up:
	_, err = cli.Ping(context.Background(), msg)
	if err != nil {
		fortio.Fatalf("grpc error from Ping0 %v", err)
	}
	for i := 1; i <= n; i++ {
		msg.Seq = int64(i)
		t1a := time.Now().UnixNano()
		msg.Ts = t1a
		res1, err := cli.Ping(context.Background(), msg)
		t2a := time.Now().UnixNano()
		if err != nil {
			fortio.Fatalf("grpc error from Ping1 %v", err)
		}
		t1b := res1.Ts
		res2, err := cli.Ping(context.Background(), msg)
		t3a := time.Now().UnixNano()
		t2b := res2.Ts
		if err != nil {
			fortio.Fatalf("grpc error from Ping2 %v", err)
		}
		rt1 := t2a - t1a
		rt2 := t3a - t2a
		rtR := t2b - t1b
		midR := t1b + (rtR / 2)
		avgRtt := (rt1 + rt2 + rtR) / 3
		x := (midR - t2a)
		fortio.Infof("Ping RTT %d (avg of %d, %d, %d ns) clock skew %d",
			avgRtt, rt1, rtR, rt2, x)
		msg = res2
	}
}

func main() {
	flag.Parse()
	if *hostFlag != "" {
		// TODO doesn't work for ipv6 addrs etc
		clientCall(fmt.Sprintf("%s:%d", *hostFlag, *portFlag), *countFlag, *payloadFlag)
	} else {
		startServer(*portFlag)
	}
}
