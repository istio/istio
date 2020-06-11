package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
	"istio.io/pkg/log"

	v2 "istio.io/istio/pilot/pkg/proxy/envoy/v2"
)

func triggerPush() {
	r, err := http.Get("http://localhost:8080/debug/adsz?push=true")
	handleErr(err)
	r.Body.Close()
	log.Infof("triggered push")
}

var (
	node = &core.Node{
Id: "sidecar~10.244.0.36~alpine-554487f8f8-sgnmm.default~default.svc.cluster.local",
}
	resourceNames = []string{"outbound|9087||shell.default.svc.cluster.local"}
	resourceNames2 = []string{"outbound|9087||shell.default.svc.cluster.local", "outbound|8080||shell.default.svc.cluster.local"}
)
func main() {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(ioutil.Discard, ioutil.Discard, ioutil.Discard))
	adsc, err := connectADS("localhost:15010")
	handleErr(err)

	// bad ACK order: OK
	//handleErr(adsc.Send(&xdsapi.DiscoveryRequest{
	//	ResponseNonce: "",
	//	Node:          node,
	//	ResourceNames: resourceNames,
	//	TypeUrl:       v2.EndpointType}))
	//log.Infof("sent request")
	//
	//resp := receive(adsc)
	//
	//triggerPush()
	//
	//respPush := receive(adsc)
	//ack(adsc, respPush.Nonce, respPush.VersionInfo)
	//ack(adsc, resp.Nonce, resp.VersionInfo)

	handleErr(adsc.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: "",
		Node:          node,
		ResourceNames: resourceNames,
		TypeUrl:       v2.EndpointType}))
	resp := receive(adsc)

	handleErr(adsc.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: "",
		Node:          node,
		ResourceNames: resourceNames2,
		TypeUrl:       v2.EndpointType}))
	resp2 := receive(adsc)

	ack(adsc, resp2.Nonce, resp2.VersionInfo)
	ack(adsc, resp.Nonce, resp.VersionInfo)

	time.Sleep(time.Second * 10)
}

func receive(adsc ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient) *xdsapi.DiscoveryResponse {
	resp, err := adsc.Recv()
	handleErr(err)
	log.Infof("got nonce=%v version=%v", resp.Nonce, resp.VersionInfo)
	return resp
}

func ack(adsc ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient, nonce, ver string) {
	handleErr(adsc.Send(&xdsapi.DiscoveryRequest{
		ResponseNonce: nonce,
		VersionInfo:   ver,
		Node:          node,
		ResourceNames: []string{"outbound|9087||shell.default.svc.cluster.local"},
		TypeUrl:       v2.EndpointType}))
	log.Infof("sent ack for nonce=%v version=%v", nonce, ver)
}

func handleErr(err error) {
	if err != nil {
		panic(err)
	}
}

func connectADS(url string) (ads.AggregatedDiscoveryService_StreamAggregatedResourcesClient, error) {
	conn, err := grpc.Dial(url, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		return nil, fmt.Errorf("GRPC dial failed: %s", err)
	}
	xds := ads.NewAggregatedDiscoveryServiceClient(conn)
	client, err := xds.StreamAggregatedResources(context.Background())
	if err != nil {
		return nil, fmt.Errorf("stream resources failed: %s", err)
	}

	return client, nil
}
