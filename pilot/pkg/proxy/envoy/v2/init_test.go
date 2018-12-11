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
package v2_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/googleapis/google/rpc"
	proto "github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/tests/util"
)

var nodeMetadata = &proto.Struct{Fields: map[string]*proto.Value{
	"ISTIO_PROXY_VERSION": {Kind: &proto.Value_StringValue{StringValue: "1.0"}}, // actual value doesn't matter
}}

// Extract cluster load assignment from a discovery response.
func getLoadAssignment(res1 *xdsapi.DiscoveryResponse) (*xdsapi.ClusterLoadAssignment, error) {
	if res1.TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
		return nil, errors.New("Invalid typeURL" + res1.TypeUrl)
	}
	if res1.Resources[0].TypeUrl != "type.googleapis.com/envoy.api.v2.ClusterLoadAssignment" {
		return nil, errors.New("Invalid resource typeURL" + res1.Resources[0].TypeUrl)
	}
	cla := &xdsapi.ClusterLoadAssignment{}
	err := cla.Unmarshal(res1.Resources[0].Value)
	if err != nil {
		return nil, err
	}
	return cla, nil
}

func testIp(id uint32) string {
	ipb := []byte{0, 0, 0, 0}
	binary.BigEndian.PutUint32(ipb, id)
	return net.IP(ipb).String()
}

func connectADS(url string) (ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient, error) {
	conn, err := grpc.Dial(url, grpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("GRPC dial failed: %s", err)
	}

	xds := ads.NewAggregatedDiscoveryServiceClient(conn)
	edsstr, err := xds.StreamAggregatedResources(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Stream resources failed: %s", err)
	}

	return edsstr, nil
}

func connectADSS(url string) (ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient, error) {
	certDir := util.IstioSrc + "/tests/testdata/certs/default/"

	clientCert, err := tls.LoadX509KeyPair(certDir+model.CertChainFilename, certDir+model.KeyFilename)
	if err != nil {
		return nil, fmt.Errorf("failed loading clients certs: %s", err)
	}

	serverCABytes, err := ioutil.ReadFile(certDir + model.RootCertFilename)
	if err != nil {
		return nil, fmt.Errorf("failed loading CA certs: %s", err)
	}
	serverCAs := x509.NewCertPool()
	if ok := serverCAs.AppendCertsFromPEM(serverCABytes); !ok {
		return nil, fmt.Errorf("failed adding CA certs to pool: %s", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      serverCAs,
		ServerName:   "istio-pilot.istio-system.svc",
	}

	creds := credentials.NewTLS(tlsCfg)

	opts := []grpc.DialOption{
		// Verify Pilot cert and service account
		grpc.WithTransportCredentials(creds),
	}
	conn, err := grpc.Dial(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("GRPC dial failed: %s", err)
	}

	xds := ads.NewAggregatedDiscoveryServiceClient(conn)
	edsstr, err := xds.StreamAggregatedResources(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Stream resources failed: %s", err)
	}
	return edsstr, nil
}

func adsReceive(ads ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient, to time.Duration) (*xdsapi.DiscoveryResponse, error) {
	done := make(chan int, 1)
	t := time.NewTimer(to)
	defer func() {
		done <- 1
	}()
	go func() {
		select {
		case <-t.C:
			_ = ads.CloseSend() // will result in adsRecv closing as well, interrupting the blocking recv
		case <-done:
			_ = t.Stop()
		}
	}()
	return ads.Recv()
}

func sendEDSReq(clusters []string, node string, edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) error {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &core.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl:       v2.EndpointType,
		ResourceNames: clusters,
	})
	if err != nil {
		return fmt.Errorf("EDS request failed: %s", err)
	}

	return nil
}

func sendEDSNack(clusters []string, node string, edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) error {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &core.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl:     v2.EndpointType,
		ErrorDetail: &rpc.Status{Message: "NOPE!"},
	})
	if err != nil {
		return fmt.Errorf("EDS NACK failed: %s", err)
	}

	return nil
}

// If pilot is reset, envoy will connect with a nonce/version info set on the previous
// connection to pilot. In HA case this may be a different pilot. This is a regression test for
// reconnect problems.
func sendEDSReqReconnect(clusters []string, edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient, res *xdsapi.DiscoveryResponse) error {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		Node: &core.Node{
			Id:       sidecarId(app3Ip, "app3"),
			Metadata: nodeMetadata,
		},
		TypeUrl:       v2.EndpointType,
		ResponseNonce: res.Nonce,
		VersionInfo:   res.VersionInfo,
		ResourceNames: clusters})
	if err != nil {
		return fmt.Errorf("EDS reconnect failed: %s", err)
	}

	return nil
}

func sendLDSReq(node string, ldsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) error {
	err := ldsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &core.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl: v2.ListenerType})
	if err != nil {
		return fmt.Errorf("LDS request failed: %s", err)
	}

	return nil
}

func sendLDSNack(node string, ldsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) error {
	err := ldsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &core.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl:     v2.ListenerType,
		ErrorDetail: &rpc.Status{Message: "NOPE!"}})
	if err != nil {
		return fmt.Errorf("LDS NACK failed: %s", err)
	}

	return nil
}

func sendRDSReq(node string, routes []string, rdsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) error {
	err := rdsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &core.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl:       v2.RouteType,
		ResourceNames: routes})
	if err != nil {
		return fmt.Errorf("RDS request failed: %s", err)
	}

	return nil
}

func sendRDSNack(node string, routes []string, rdsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) error {
	err := rdsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &core.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl:     v2.RouteType,
		ErrorDetail: &rpc.Status{Message: "NOPE!"}})
	if err != nil {
		return fmt.Errorf("RDS NACK failed: %s", err)
	}

	return nil
}

func sendCDSReq(node string, edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) error {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &core.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl: v2.ClusterType})
	if err != nil {
		return fmt.Errorf("CDS request failed: %s", err)
	}

	return nil
}

func sendCDSNack(node string, edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) error {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &core.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		ErrorDetail: &rpc.Status{Message: "NOPE!"},
		TypeUrl:     v2.ClusterType})
	if err != nil {
		return fmt.Errorf("CDS NACK failed: %s", err)
	}

	return nil
}
