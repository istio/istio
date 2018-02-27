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
	"flag"
	"log"
	"net"
	"time"

	api "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/auth"
	sds "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/proto"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	tls                = flag.Bool("tls", false, "Connection uses TLS if true, else plain TCP")
	caFile             = flag.String("ca_file", "", "The file containning the CA root cert file")
	serverHostOverride = flag.String("server_host_override", "x.test.youtube.com", "The server name use to verify the hostname returned by TLS handshake")
	udsPath            = flag.String("uds_path", "sock", "Unix Domain Socket file path name")
)

func unixDialer(target string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", target, timeout)
}

func main() {
	flag.Parse()
	var opts []grpc.DialOption
	if *tls {
		creds, err := credentials.NewClientTLSFromFile(*caFile, *serverHostOverride)
		if err != nil {
			log.Fatalf("Failed to create TLS credentials %v", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithInsecure())
	}
	opts = append(opts, grpc.WithDialer(unixDialer))
	conn, err := grpc.Dial(*udsPath, opts...)
	if err != nil {
		log.Fatalf("failed to connect with server %v", err)
	}
	client := sds.NewSecretDiscoveryServiceClient(conn)
	response, _ := client.FetchSecrets(context.Background(), &api.DiscoveryRequest{})

	var secret auth.Secret
	resource := response.GetResources()[0]
	bytes := resource.Value

	err = proto.Unmarshal(bytes, &secret)
	if err != nil {
		log.Fatalf("failed parse the response %v", err)
	}

	log.Println("Received secrets:")
	log.Printf("version info: %v, TypeUrl: %v, secret name: %v, certificate: %v",
		response.GetVersionInfo(), response.GetTypeUrl(), secret.GetName(), secret.GetTlsCertificate())
	_ = conn.Close()
}
