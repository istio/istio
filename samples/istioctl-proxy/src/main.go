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
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"golang.org/x/oauth2"
	"istio.io/istio/istioctl/pkg/clioptions"
	"istio.io/istio/istioctl/pkg/multixds"
	"istio.io/istio/istioctl/pkg/xds"
	pilotxds "istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pkg/kube"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
)

var (
	serverPort    = flag.String("port", "15012", "The gRPC server port")
	serverCert    = flag.String("cert", "/tmp/certs/localhost.crt", "The name of server's certificate file")
	serverkey     = flag.String("key", "/tmp/certs/localhost.key", "The name of server's private key file")
	istioRevision = flag.String("revision", "", "The istiod revision to call")
)

type IstioctlServer struct {
	istioNamespace string
}

func main() {
	flag.Parse()

	namespace := os.Getenv("POD_NAMESPACE")
	if len(namespace) == 0 {
		namespace = "external-istiod"
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%s", *serverPort))
	if err != nil {
		log.Println(err)
		log.Fatalf("failed to listen: %v", err)
	}

	log.Printf("Listening on port: %s", *serverPort)

	var grpcServer *grpc.Server
	creds, _ := credentials.NewServerTLSFromFile(*serverCert, *serverkey)
	grpcServer = grpc.NewServer(grpc.Creds(creds))

	s := &IstioctlServer{namespace}
	xdsapi.RegisterAggregatedDiscoveryServiceServer(grpcServer, s)
	reflection.Register(grpcServer)

	log.Printf("Starting to serve with cert: %s, key: %s ...", *serverCert, *serverkey)

	err = grpcServer.Serve(lis)
	if err != nil {
		log.Fatalf("failed to serve: %s", err)
	}
}

func (s *IstioctlServer) StreamAggregatedResources(in xdsapi.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	request, err := in.Recv()
	if err != nil {
		return err
	}

	queryAll := false
	if request.TypeUrl == "istio.io/debug/syncz" || request.TypeUrl == "istio.io/connections" {
		queryAll = true
	} else if request.TypeUrl != "istio.io/debug/config_dump" {
		return fmt.Errorf("unsupported StreamAggregatedResources typeURL: %s", request.TypeUrl)
	}

	xdsResponses, err := queryIstiodPods(queryAll, request, s.istioNamespace, in.Context())
	if err != nil {
		return err
	}

	response := xdsapi.DiscoveryResponse{TypeUrl: request.TypeUrl}
	for _, xdsResponse := range xdsResponses {
		if response.ControlPlane == nil {
			cpInstance := pilotxds.IstioControlPlaneInstance{}
			if err := json.Unmarshal([]byte(xdsResponse.ControlPlane.Identifier), &cpInstance); err == nil {
				cpInstance.ID = "<external>"
				identifier, err := json.Marshal(cpInstance)
				if err == nil {
					response.ControlPlane = &corev3.ControlPlane{Identifier: string(identifier[:])}
				}
			}
			response.Nonce = xdsResponse.Nonce
		}
		response.Resources = append(response.Resources, xdsResponse.Resources...)
	}

	err = in.Send(&response)
	if err != nil {
		return err
	}

	return nil
}

func (s *IstioctlServer) DeltaAggregatedResources(xdsapi.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	return fmt.Errorf("unsupported API: DeltaAggregatedResources")
}

func queryIstiodPods(all bool, dr *xdsapi.DiscoveryRequest, namespace string, ctx context.Context) ([]*xdsapi.DiscoveryResponse, error) {
	kubeClient, err := kube.NewExtendedClient(kube.BuildClientCmd("", ""), *istioRevision)
	if err != nil {
		return nil, err
	}

	pods, err := kubeClient.GetIstioPods(context.TODO(), namespace, map[string]string{
		"labelSelector": "app=istiod",
		"fieldSelector": "status.phase=Running",
	})
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, multixds.ControlPlaneNotFoundError{Namespace: namespace}
	}

	xdsOpts := clioptions.CentralControlPlaneOptions{
		XDSSAN:  makeSan(namespace, kubeClient.Revision()),
		Timeout: 2e9,
	}
	dialOpts := getDialOptions(ctx)

	responses := []*xdsapi.DiscoveryResponse{}
	for _, pod := range pods {
		fw, err := kubeClient.NewPortForwarder(pod.Name, pod.Namespace, "localhost", 0, 15012)
		if err != nil {
			return nil, err
		}
		err = fw.Start()
		if err != nil {
			return nil, err
		}
		defer fw.Close()
		xdsOpts.Xds = fw.Address()
		response, err := xds.GetXdsResponse(dr, namespace, "default", xdsOpts, dialOpts)
		if err != nil {
			return nil, fmt.Errorf("could not get XDS from discovery pod %q: %v", pod.Name, err)
		}
		responses = append(responses, response)
		if !all && len(responses) > 0 {
			break
		}
	}
	return responses, nil
}

func makeSan(namespace, revision string) string {
	if revision == "" {
		return fmt.Sprintf("istiod.%s.svc", namespace)
	}
	return fmt.Sprintf("istiod-%s.%s.svc", revision, namespace)
}

func getDialOptions(ctx context.Context) []grpc.DialOption {
	md, _ := metadata.FromIncomingContext(ctx)
	auth := md.Get("authorization")[0]
	//log.Printf("authorization header: %s", auth)

	token := oauth2.Token{
		AccessToken: strings.TrimPrefix(auth, "Bearer "),
	}
	perRPC := oauth.NewOauthAccess(&token)

	return []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(
			&tls.Config{
				InsecureSkipVerify: true,
			})),
		grpc.WithPerRPCCredentials(perRPC),
	}
}
