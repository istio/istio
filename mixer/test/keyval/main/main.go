// Copyright Istio Authors
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
	"net"

	"google.golang.org/grpc"

	"istio.io/istio/mixer/test/keyval"
)

func main() {
	listener, err := net.Listen("tcp", ":9070")
	if err != nil {
		panic(err)
	}
	server := grpc.NewServer()
	keyval.RegisterHandleKeyvalServiceServer(server, keyval.Keyval{})
	_ = server.Serve(listener)
}
