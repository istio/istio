// Copyright 2017 Istio Authors
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

// An example implementation of an echo backend.

package main

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	flag "github.com/spf13/pflag"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	pb "istio.io/manager/test/grpcecho"
)

var (
	ports     []int
	grpcPorts []int
	version   string
)

func init() {
	flag.IntSliceVar(&ports, "port", []int{8080}, "HTTP/1.1 ports")
	flag.IntSliceVar(&grpcPorts, "grpc", []int{7070}, "GRPC ports")
	flag.StringVar(&version, "version", "", "Version string")
}

type handler struct {
	port int
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := bytes.Buffer{}
	body.WriteString("ServiceVersion=" + version + "\n")
	body.WriteString("ServicePort=" + strconv.Itoa(h.port) + "\n")
	body.WriteString("Method=" + r.Method + "\n")
	body.WriteString("URL=" + r.URL.String() + "\n")
	body.WriteString("Proto=" + r.Proto + "\n")
	body.WriteString("RemoteAddr=" + r.RemoteAddr + "\n")
	body.WriteString("Host=" + r.Host + "\n")
	for name, headers := range r.Header {
		for _, h := range headers {
			body.WriteString(fmt.Sprintf("%v=%v\n", name, h))
		}
	}
	w.Header().Set("Content-Type", "application/text")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body.Bytes()); err != nil {
		log.Println(err.Error())
	}
}

func (h handler) Echo(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	body := bytes.Buffer{}
	md, ok := metadata.FromContext(ctx)
	if ok {
		for key, vals := range md {
			body.WriteString(key + "=" + strings.Join(vals, " ") + "\n")
		}
	}
	body.WriteString("ServiceVersion=" + version + "\n")
	body.WriteString("ServicePort=" + strconv.Itoa(h.port) + "\n")
	body.WriteString("Echo=" + req.GetMessage())
	return &pb.EchoResponse{Message: body.String()}, nil
}

func runHTTP(port int) {
	fmt.Printf("Listening HTTP1.1 on %v\n", port)
	h := handler{port: port}
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), h); err != nil {
		log.Println(err.Error())
	}
}

func runGRPC(port int) {
	fmt.Printf("Listening GRPC on %v\n", port)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	h := handler{port: port}
	grpcServer := grpc.NewServer()
	pb.RegisterEchoTestServiceServer(grpcServer, &h)
	if err = grpcServer.Serve(lis); err != nil {
		log.Println(err.Error())
	}
}

func main() {
	flag.Parse()
	for _, port := range ports {
		go runHTTP(port)
	}
	for _, grpcPort := range grpcPorts {
		go runGRPC(grpcPort)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs
}
