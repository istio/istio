package main

import (
	"log"
	"net"

	"google.golang.org/grpc"

	v3 "github.com/envoyproxy/go-control-plane/envoy/service/accesslog/v3"
)

func main() {
	grpcServer := grpc.NewServer()
	v3.RegisterAccessLogServiceServer(grpcServer, newSink())

	l, err := net.Listen("tcp", ":9001")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	adminServer, err := newAdminServer()
	if err != nil {
		log.Fatalf("failed to init admin server: %v", err)
	}

	go func() {
		if err := adminServer.Start(); err != nil {
			log.Fatalf("failed to start admin server: %v", err)
		}
	}()

	log.Println("Listening on tcp://localhost:9001")
	grpcServer.Serve(l)
}
