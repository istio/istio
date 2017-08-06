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

// An example implementation of a client.

package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang/sync/errgroup"
	"google.golang.org/grpc"
	pb "istio.io/pilot/test/grpcecho"
)

var (
	count   int
	timeout time.Duration

	url       string
	headerKey string
	headerVal string
)

const (
	hostKey = "Host"
)

func init() {
	flag.IntVar(&count, "count", 1, "Number of times to make the request")
	flag.DurationVar(&timeout, "timeout", 15*time.Second, "Request timeout")
	flag.StringVar(&url, "url", "", "Specify URL")
	flag.StringVar(&headerKey, "key", "", "Header key (use Host for authority)")
	flag.StringVar(&headerVal, "val", "", "Header value")
}

func makeHTTPRequest(client *http.Client) func(int) func() error {
	return func(i int) func() error {
		return func() error {
			req, err := http.NewRequest("GET", url, nil)
			if err != nil {
				return err
			}

			log.Printf("[%d] Url=%s\n", i, url)
			if headerKey == hostKey {
				req.Host = headerVal
				log.Printf("[%d] Host=%s\n", i, headerVal)
			} else if headerKey != "" {
				req.Header.Add(headerKey, headerVal)
				log.Printf("[%d] Header=%s:%s\n", i, headerKey, headerVal)
			}

			resp, err := client.Do(req)
			if err != nil {
				return err
			}

			log.Printf("[%d] StatusCode=%d\n", i, resp.StatusCode)

			data, err := ioutil.ReadAll(resp.Body)
			defer func() {
				if err = resp.Body.Close(); err != nil {
					log.Printf("[%d error] %s\n", i, err)
				}
			}()

			if err != nil {
				return err
			}

			for _, line := range strings.Split(string(data), "\n") {
				if line != "" {
					log.Printf("[%d body] %s\n", i, line)
				}
			}

			return nil
		}
	}
}

func makeGRPCRequest(client pb.EchoTestServiceClient) func(int) func() error {
	return func(i int) func() error {
		return func() error {
			req := &pb.EchoRequest{Message: fmt.Sprintf("request #%d", i)}
			log.Printf("[%d] grpcecho.Echo(%v)\n", i, req)
			resp, err := client.Echo(context.Background(), req)
			if err != nil {
				return err
			}

			// when the underlying HTTP2 request returns status 404, GRPC
			// request does not return an error in grpc-go.
			// instead it just returns an empty response
			for _, line := range strings.Split(resp.GetMessage(), "\n") {
				if line != "" {
					log.Printf("[%d body] %s\n", i, line)
				}
			}
			return nil
		}
	}
}

func main() {
	flag.Parse()
	var f func(int) func() error
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		/* #nosec */
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
			Timeout: timeout,
		}
		f = makeHTTPRequest(client)
	} else if strings.HasPrefix(url, "grpc://") {
		address := url[len("grpc://"):]

		// grpc-go sets incorrect authority header
		authority := address
		if headerKey == hostKey {
			authority = headerVal
		}

		conn, err := grpc.Dial(address,
			grpc.WithInsecure(),
			grpc.WithAuthority(authority),
			grpc.WithBlock(),
			grpc.WithTimeout(timeout))
		if err != nil {
			log.Fatalf("did not connect: %v", err)
		}
		defer func() {
			if err := conn.Close(); err != nil {
				log.Println(err)
			}
		}()
		client := pb.NewEchoTestServiceClient(conn)
		f = makeGRPCRequest(client)
	} else {
		log.Fatalf("Unrecognized protocol %q", url)
	}

	g, _ := errgroup.WithContext(context.Background())
	for i := 0; i < count; i++ {
		g.Go(f(i))
	}
	if err := g.Wait(); err != nil {
		log.Printf("Error %s\n", err)
		os.Exit(1)
	}

	log.Println("All requests succeeded")
}
