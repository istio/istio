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
package xds_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"
	structpb "github.com/golang/protobuf/ptypes/struct"

	"istio.io/istio/pilot/pkg/model"

	"google.golang.org/genproto/googleapis/rpc/status"

	v3 "istio.io/istio/pilot/pkg/xds/v3"
)

type AdsClient discovery.AggregatedDiscoveryService_StreamAggregatedResourcesClient

var nodeMetadata = &structpb.Struct{Fields: map[string]*structpb.Value{
	"ISTIO_VERSION": {Kind: &structpb.Value_StringValue{StringValue: "1.3"}}, // actual value doesn't matter
}}

func getLoadAssignment(res1 *discovery.DiscoveryResponse) (*endpoint.ClusterLoadAssignment, error) {
	if res1.TypeUrl != v3.EndpointType {
		return nil, errors.New("Invalid typeURL" + res1.TypeUrl)
	}
	if res1.Resources[0].TypeUrl != v3.EndpointType {
		return nil, errors.New("Invalid resource typeURL" + res1.Resources[0].TypeUrl)
	}
	cla := &endpoint.ClusterLoadAssignment{}
	err := ptypes.UnmarshalAny(res1.Resources[0], cla)
	if err != nil {
		return nil, err
	}
	return cla, nil
}

func testIP(id uint32) string {
	ipb := []byte{0, 0, 0, 0}
	binary.BigEndian.PutUint32(ipb, id)
	return net.IP(ipb).String()
}

func adsReceive(ads AdsClient, to time.Duration) (*discovery.DiscoveryResponse, error) {
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

func sendEDSReq(clusters []string, node string, version, nonce string, edsClient AdsClient) error {
	err := edsClient.Send(&discovery.DiscoveryRequest{
		ResponseNonce: nonce,
		VersionInfo:   version,
		Node: &corev3.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl:       v3.EndpointType,
		ResourceNames: clusters,
	})
	if err != nil {
		return fmt.Errorf("EDS request failed: %s", err)
	}

	return nil
}

func sendEDSNack(_ []string, node string, client AdsClient) error {
	return sendXds(node, client, v3.EndpointType, "NOPE!")
}

// If pilot is reset, envoy will connect with a nonce/version info set on the previous
// connection to pilot. In HA case this may be a different pilot. This is a regression test for
// reconnect problems.
func sendEDSReqReconnect(clusters []string, client AdsClient, res *discovery.DiscoveryResponse) error {
	err := client.Send(&discovery.DiscoveryRequest{
		Node: &corev3.Node{
			Id:       sidecarID(app3Ip, "app3"),
			Metadata: nodeMetadata,
		},
		TypeUrl:       v3.EndpointType,
		ResponseNonce: res.Nonce,
		VersionInfo:   res.VersionInfo,
		ResourceNames: clusters})
	if err != nil {
		return fmt.Errorf("EDS reconnect failed: %s", err)
	}

	return nil
}

func sendLDSReq(node string, client AdsClient) error {
	return sendXds(node, client, v3.ListenerType, "")
}

func sendLDSReqWithLabels(node string, ldsclient AdsClient, labels map[string]string) error {
	err := ldsclient.Send(&discovery.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &corev3.Node{
			Id:       node,
			Metadata: model.NodeMetadata{Labels: labels}.ToStruct(),
		},
		TypeUrl: v3.ListenerType})
	if err != nil {
		return fmt.Errorf("LDS request failed: %s", err)
	}

	return nil
}

func sendLDSNack(node string, client AdsClient) error {
	return sendXds(node, client, v3.ListenerType, "NOPE!")
}

func sendRDSReq(node string, routes []string, version, nonce string, rdsclient AdsClient) error {
	err := rdsclient.Send(&discovery.DiscoveryRequest{
		ResponseNonce: nonce,
		VersionInfo:   version,
		Node: &corev3.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl:       v3.RouteType,
		ResourceNames: routes})
	if err != nil {
		return fmt.Errorf("RDS request failed: %s", err)
	}

	return nil
}

func sendRDSNack(node string, _ []string, nonce string, rdsclient AdsClient) error {
	err := rdsclient.Send(&discovery.DiscoveryRequest{
		ResponseNonce: nonce,
		Node: &corev3.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		TypeUrl:     v3.RouteType,
		ErrorDetail: &status.Status{Message: "NOPE!"}})
	if err != nil {
		return fmt.Errorf("RDS NACK failed: %s", err)
	}

	return nil
}

func sendCDSReq(node string, client AdsClient) error {
	return sendXds(node, client, v3.ClusterType, "")
}

func sendCDSNack(node string, client AdsClient) error {
	return sendXds(node, client, v3.ClusterType, "NOPE!")
}

func sendXds(node string, client AdsClient, typeURL string, errMsg string) error {
	var errorDetail *status.Status
	if errMsg != "" {
		errorDetail = &status.Status{Message: errMsg}
	}
	err := client.Send(&discovery.DiscoveryRequest{
		ResponseNonce: time.Now().String(),
		Node: &corev3.Node{
			Id:       node,
			Metadata: nodeMetadata,
		},
		ErrorDetail: errorDetail,
		TypeUrl:     typeURL})
	if err != nil {
		return fmt.Errorf("%v Request failed: %s", typeURL, err)
	}

	return nil
}
