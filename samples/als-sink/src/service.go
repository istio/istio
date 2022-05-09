package main

import (
	"io"
	"log"

	v3 "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v3"
)

type server struct {
}

type Metadata struct {
	LogType        string
	UpstreamPeer   string
	DownstreamPeer string
}

var _ v3.AccessLogServiceServer = &server{}
var cache map[string][]Metadata = make(map[string][]Metadata)

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
		if in.GetIdentifier().GetNode() == nil {
			continue
		}
		for _, tle := range in.GetTcpLogs().GetLogEntry() {
			upstreamPeer := tle.GetCommonProperties().FilterStateObjects["wasm.upstream_peer_id"].String()
			downstreamPeer := tle.GetCommonProperties().FilterStateObjects["wasm.downstream_peer_id"].String()
			cache[in.Identifier.Node.Id] = append(cache[in.Identifier.Node.Id], Metadata{LogType: "tcpMx", UpstreamPeer: upstreamPeer, DownstreamPeer: downstreamPeer})
			log.Printf("TCP METADATA: %+v, filterstateObjects: %+v", tle.GetCommonProperties().Metadata, tle.GetCommonProperties().FilterStateObjects)
		}
		for _, hle := range in.GetHttpLogs().GetLogEntry() {
			upstreamPeer := hle.GetCommonProperties().FilterStateObjects["wasm.upstream_peer_id"].String()
			downstreamPeer := hle.GetCommonProperties().FilterStateObjects["wasm.downstream_peer_id"].String()
			cache[in.Identifier.Node.Id] = append(cache[in.Identifier.Node.Id], Metadata{LogType: "httpMx", UpstreamPeer: upstreamPeer, DownstreamPeer: downstreamPeer})
			log.Printf("HTTP METADATA: %+v, filterstateObjects: %+v", hle.GetCommonProperties().Metadata, hle.GetCommonProperties().FilterStateObjects)
			log.Printf("cache: %+v", cache)
		}
	}
}
