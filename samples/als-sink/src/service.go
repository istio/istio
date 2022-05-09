// Copyright Istio Authors. All Rights Reserved.
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
	"io"
	"log"
	"strings"
	"sync"

	v31 "github.com/envoyproxy/go-control-plane/envoy/data/accesslog/v3"
	v3 "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v3"
)

type server struct {
}

var _ v3.AccessLogServiceServer = &server{}

// Metadata represents metadata received about peer envoy in a single access log(grpc message)
type Metadata struct {
	// LogType is httpMx or tcpMx, which shows type of traffic for
	// which access log is received
	LogType string
	// UpstreamPeer is the server envoy
	UpstreamPeer string
	// UpstreamPeer is the client envoy
	DownstreamPeer string
}

//
var cache map[string]Metadata = make(map[string]Metadata)
var cacheLock sync.RWMutex

// NewSink AccessLogServiceServer
func newSink() v3.AccessLogServiceServer {
	return &server{}
}

func (s *server) StreamAccessLogs(stream v3.AccessLogService_StreamAccessLogsServer) error {
	log.Println("Started stream")
	for {
		in, err := stream.Recv()
		log.Println("Received value")
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		for _, tle := range in.GetTcpLogs().GetLogEntry() {
			upstreamCluster := tle.GetCommonProperties().GetUpstreamCluster()
			SANs := tle.GetCommonProperties().GetTlsProperties().GetLocalCertificateProperties().GetSubjectAltName()
			alsSource := getALSSource(SANs, upstreamCluster, "tcpMx")
			upstreamPeer := tle.GetCommonProperties().FilterStateObjects["wasm.upstream_peer_id"].String()
			downstreamPeer := tle.GetCommonProperties().FilterStateObjects["wasm.downstream_peer_id"].String()

			cacheLock.Lock()
			cache[alsSource] = Metadata{LogType: "tcpMx", UpstreamPeer: upstreamPeer, DownstreamPeer: downstreamPeer}
			cacheLock.Unlock()
		}
		for _, hle := range in.GetHttpLogs().GetLogEntry() {
			upstreamCluster := hle.GetCommonProperties().GetUpstreamCluster()
			SANs := hle.GetCommonProperties().GetTlsProperties().GetLocalCertificateProperties().GetSubjectAltName()
			alsSource := getALSSource(SANs, upstreamCluster, "httpMx")
			upstreamPeer := hle.GetCommonProperties().FilterStateObjects["wasm.upstream_peer_id"].String()
			downstreamPeer := hle.GetCommonProperties().FilterStateObjects["wasm.downstream_peer_id"].String()

			cacheLock.Lock()
			cache[alsSource] = Metadata{LogType: "httpMx", UpstreamPeer: upstreamPeer, DownstreamPeer: downstreamPeer}
			cacheLock.Unlock()
		}
	}
}

func getALSSource(SANs []*v31.TLSProperties_CertificateProperties_SubjectAltName, upstreamCluster string, ALSType string) string {
	// TODO(vikasc): Improve this logic of deciding alsSource. Instead of generic "client" and "server",
	// fill specific identity like pod IP
	isEgressGateway := false
	if len(SANs) > 0 {
		SANString := SANs[0].String()
		if strings.Contains(SANString, "egressgateway") {
			isEgressGateway = true
		}
	}
	alsSource := "client"

	// ex: "upstreamCluster":"inbound|8888||"
	if strings.Contains(upstreamCluster, "inbound") || isEgressGateway {
		alsSource = "server"
	}

	return ALSType + "-" + alsSource
}
