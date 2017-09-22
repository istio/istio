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

package main

import (
	"flag"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"

	"istio.io/pilot/test/mixer"
	"istio.io/pilot/test/mixer/pb"
)

var (
	port int
)

func init() {
	flag.IntVar(&port, "port", 9091, "HTTP/2 port")
}

func main() {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatal(err)
	}
	grpcServer := grpc.NewServer()
	instance := mixer.NewServer()
	istio_mixer_v1.RegisterMixerServer(grpcServer, instance)
	if err = grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
