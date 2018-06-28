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
	"io/ioutil"
	"net"
	"testing"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	envoy_api_v2_core1 "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"

	"github.com/gogo/googleapis/google/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v2"
	"istio.io/istio/tests/util"
)

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

func connectADS(t *testing.T, url string) ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient {
	conn, err := grpc.Dial(url, grpc.WithInsecure())
	if err != nil {
		t.Fatal("Connection failed", err)
	}

	xds := ads.NewAggregatedDiscoveryServiceClient(conn)
	edsstr, err := xds.StreamAggregatedResources(context.Background())
	if err != nil {
		t.Fatal("Rpc failed", err)
	}
	return edsstr
}

func connectADSS(t *testing.T, url string) ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient {
	certDir := util.IstioSrc + "/tests/testdata/certs/default/"

	clientCert, err := tls.LoadX509KeyPair(certDir+model.CertChainFilename,
		certDir+model.KeyFilename)
	if err != nil {
		t.Fatal("Can't load client certs ", err)
		return nil
	}

	serverCABytes, err := ioutil.ReadFile(certDir + model.RootCertFilename)
	if err != nil {
		t.Fatal("Can't load client certs ", err)
		return nil
	}
	serverCAs := x509.NewCertPool()
	if ok := serverCAs.AppendCertsFromPEM(serverCABytes); !ok {
		t.Fatal("Can't load client certs ", err)
		return nil
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
		t.Fatal("Connection failed", err)
	}

	xds := ads.NewAggregatedDiscoveryServiceClient(conn)
	edsstr, err := xds.StreamAggregatedResources(context.Background())
	if err != nil {
		t.Fatal("Rpc failed", err)
	}
	return edsstr
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

func sendEDSReq(t *testing.T, clusters []string, node string, edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &envoy_api_v2_core1.Node{
			Id: node,
		},
		TypeUrl:       v2.EndpointType,
		ResourceNames: clusters,
	})
	if err != nil {
		t.Fatal("Send failed", err)
	}
}

func sendEDSNack(t *testing.T, clusters []string, node string, edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &envoy_api_v2_core1.Node{
			Id: node,
		},
		TypeUrl:     v2.EndpointType,
		ErrorDetail: &rpc.Status{Message: "NOPE!"},
	})
	if err != nil {
		t.Fatal("Send failed", err)
	}
}

// If pilot is reset, envoy will connect with a nonce/version info set on the previous
// connection to pilot. In HA case this may be a different pilot. This is a regression test for
// reconnect problems.
func sendEDSReqReconnect(t *testing.T, clusters []string,
	edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient,
	res *xdsapi.DiscoveryResponse) {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		Node: &envoy_api_v2_core1.Node{
			Id: sidecarId(app3Ip, "app3"),
		},
		TypeUrl:       v2.EndpointType,
		ResponseNonce: res.Nonce,
		VersionInfo:   res.VersionInfo,
		ResourceNames: clusters})
	if err != nil {
		t.Fatal("Send failed", err)
	}
}

func sendLDSReq(t *testing.T, node string, ldsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	err := ldsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &envoy_api_v2_core1.Node{
			Id: node,
		},
		TypeUrl: v2.ListenerType})
	if err != nil {
		t.Fatal("Send failed", err)
	}
}

func sendLDSNack(t *testing.T, node string, ldsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	err := ldsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &envoy_api_v2_core1.Node{
			Id: node,
		},
		TypeUrl:     v2.ListenerType,
		ErrorDetail: &rpc.Status{Message: "NOPE!"}})
	if err != nil {
		t.Fatal("Send failed", err)
	}
}

func sendRDSReq(t *testing.T, node string, routes []string, rdsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	err := rdsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &envoy_api_v2_core1.Node{
			Id: node,
		},
		TypeUrl:       v2.RouteType,
		ResourceNames: routes})
	if err != nil {
		t.Fatal("Send failed", err)
	}

}
func sendRDSNack(t *testing.T, node string, routes []string, rdsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	err := rdsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &envoy_api_v2_core1.Node{
			Id: node,
		},
		TypeUrl:     v2.RouteType,
		ErrorDetail: &rpc.Status{Message: "NOPE!"}})
	if err != nil {
		t.Fatal("Send failed", err)
	}
}

func sendCDSReq(t *testing.T, node string, edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &envoy_api_v2_core1.Node{
			Id: node,
		},
		TypeUrl: v2.ClusterType})
	if err != nil {
		t.Fatal("Send failed", err)
	}
}

func sendCDSNack(t *testing.T, node string, edsstr ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) {
	err := edsstr.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &envoy_api_v2_core1.Node{
			Id: node,
		},
		ErrorDetail: &rpc.Status{Message: "NOPE!"},
		TypeUrl:     v2.ClusterType})
	if err != nil {
		t.Fatal("Send failed", err)
	}
}
