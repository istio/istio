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
//
// To test flaky HTTP service recovery, this backend has a special features.
// If the "?codes=" query parameter is used it will return HTTP response codes other than 200
// according to a probability distribution.
// For example, ?codes=500:90,200:10 returns 500 90% of times and 200 10% of times
// For example, ?codes=500:1,200:1 returns 500 50% of times and 200 50% of times
// For example, ?codes=501:999,401:1 returns 500 99.9% of times and 401 0.1% of times.
// For example, ?codes=500,200 returns 500 50% of times and 200 50% of times

package main

import (
	"bytes"
	"fmt"
	"log"
	"math/rand"
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
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	pb "istio.io/pilot/test/grpcecho"
)

var (
	ports     []int
	grpcPorts []int
	version   string

	crt, key string
)

func init() {
	flag.IntSliceVar(&ports, "port", []int{8080}, "HTTP/1.1 ports")
	flag.IntSliceVar(&grpcPorts, "grpc", []int{7070}, "GRPC ports")
	flag.StringVar(&version, "version", "", "Version string")
	flag.StringVar(&crt, "crt", "", "gRPC TLS server-side certificate")
	flag.StringVar(&key, "key", "", "gRPC TLS server-side key")
}

type handler struct {
	port int
}

// Imagine a pie of different flavors.
// The flavors are the HTTP response codes.
// The chance of a particular flavor is ( slices / sum of slices ).
type codeAndSlices struct {
	httpResponseCode int
	slices           int
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := bytes.Buffer{}

	if err := r.ParseForm(); err != nil {
		body.WriteString("ParseForm() error: " + err.Error() + "\n")
	}

	// If the request has form ?codes=code[:chance][,code[:chance]]* return those codes, rather than 200
	// For example, ?codes=500:1,200:1 returns 500 1/2 times and 200 1/2 times
	// For example, ?codes=500:90,200:10 returns 500 90% of times and 200 10% of times
	if err := setResponseFromCodes(r, w); err != nil {
		body.WriteString("codes error: " + err.Error() + "\n")
	}

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

	if hostname, err := os.Hostname(); err == nil {
		body.WriteString(fmt.Sprintf("Hostname=%v\n", hostname))
	}

	w.Header().Set("Content-Type", "application/text")
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

	var grpcServer *grpc.Server
	if crt != "" && key != "" {
		// Create the TLS credentials
		creds, errCreds := credentials.NewServerTLSFromFile(crt, key)
		if errCreds != nil {
			log.Fatalf("could not load TLS keys: %s", errCreds)
		}
		grpcServer = grpc.NewServer(grpc.Creds(creds))
	} else {
		grpcServer = grpc.NewServer()
	}
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

func setResponseFromCodes(request *http.Request, response http.ResponseWriter) error {
	responseCodes := request.FormValue("codes")

	codes, err := validateCodes(responseCodes)
	if err != nil {
		return err
	}

	// Choose a random "slice" from a pie
	var totalSlices = 0
	for _, flavor := range codes {
		totalSlices += flavor.slices
	}
	slice := rand.Intn(totalSlices)

	// What flavor is that slice?
	responseCode := codes[len(codes)-1].httpResponseCode // Assume the last slice
	position := 0
	for n, flavor := range codes {
		if position > slice {
			responseCode = codes[n-1].httpResponseCode // No, use an earlier slice
			break
		}
		position += flavor.slices
	}

	response.WriteHeader(responseCode)
	return nil
}

// codes must be comma-separated HTTP response code, colon, positive integer
func validateCodes(codestrings string) ([]codeAndSlices, error) {
	if codestrings == "" {
		// Consider no codes to be "200:1" -- return HTTP 200 100% of the time.
		codestrings = strconv.Itoa(http.StatusOK) + ":1"
	}

	aCodestrings := strings.Split(codestrings, ",")
	codes := make([]codeAndSlices, len(aCodestrings))

	for i, codestring := range aCodestrings {
		codeAndSlice, err := validateCodeAndSlices(codestring)
		if err != nil {
			return []codeAndSlices{{http.StatusBadRequest, 1}}, err
		}
		codes[i] = codeAndSlice
	}

	return codes, nil
}

// code must be HTTP response code
func validateCodeAndSlices(codecount string) (codeAndSlices, error) {
	flavor := strings.Split(codecount, ":")

	// Demand code or code:number
	if len(flavor) == 0 || len(flavor) > 2 {
		return codeAndSlices{http.StatusBadRequest, 9999}, fmt.Errorf("invalid %q (want code or code:count)", codecount)
	}

	n, err := strconv.Atoi(flavor[0])
	if err != nil {
		return codeAndSlices{http.StatusBadRequest, 9999}, err
	}

	if n < http.StatusOK || n >= 600 {
		return codeAndSlices{http.StatusBadRequest, 9999}, fmt.Errorf("invalid HTTP response code %v", n)
	}

	count := 1
	if len(flavor) > 1 {
		count, err = strconv.Atoi(flavor[1])
		if err != nil {
			return codeAndSlices{http.StatusBadRequest, 9999}, err
		}
		if count < 0 {
			return codeAndSlices{http.StatusBadRequest, 9999}, fmt.Errorf("invalid count %v", count)
		}
	}

	return codeAndSlices{n, count}, nil
}
