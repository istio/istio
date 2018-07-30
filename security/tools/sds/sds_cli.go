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

package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	disapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
	"istio.io/istio/security/pkg/nodeagent/sds"
)

var fakeCredentialToken = "Bearer fakeToken"

func sdsRequest(socket string, sdsCert string, req *xdsapi.DiscoveryRequest) *xdsapi.DiscoveryResponse {
	var opts []grpc.DialOption

	if sdsCert != "" {
		creds, err := credentials.NewClientTLSFromFile(sdsCert, "")
		if err != nil {
			panic(err.Error())
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}

	opts = append(opts, grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout("unix", socket, timeout)
	}))

	conn, err := grpc.Dial(socket, opts...)
	if err != nil {
		panic(err.Error())
	}
	defer conn.Close()

	sdsClient := disapi.NewSecretDiscoveryServiceClient(conn)
	header := metadata.Pairs(sds.CredentialTokenHeaderKey, fakeCredentialToken)
	ctx := metadata.NewOutgoingContext(context.Background(), header)
	stream, err := sdsClient.StreamSecrets(ctx)
	if err != nil {
		panic(err.Error())
	}
	err = stream.Send(req)
	if err != nil {
		panic(err.Error())
	}
	res, err := stream.Recv()
	if err != nil {
		panic(err.Error())
	}
	return res
}

func main() {
	sdsSocket := flag.String("socket", "/var/run/sds/uds_path", "SDS socket")
	outputFile := flag.String("out", "", "output file. Leave blank to go to stdout")

	// Local test through TLS using /etc/istio/nodeagent-root-cert.pem
	sdsCertFile := flag.String("certFile", "", "cert file used to send secure gRPC request to SDS server")

	flag.Parse()

	req := &xdsapi.DiscoveryRequest{
		ResourceNames: []string{"spiffe://cluster.local/ns/bar/sa/foo"},
		Node: &core.Node{
			Id: "sidecar~127.0.0.1~id~local",
		},
	}
	resp := sdsRequest(*sdsSocket, *sdsCertFile, req)

	strResponse, _ := model.ToJSONWithIndent(resp, " ")
	if outputFile == nil || *outputFile == "" {
		fmt.Printf("%v\n", strResponse)
	} else {
		if err := ioutil.WriteFile(*outputFile, []byte(strResponse), 0644); err != nil {
			log.Errorf("Cannot write output to file %q", *outputFile)
		}
	}
}
