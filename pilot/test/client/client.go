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
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "istio.io/pilot/test/grpcecho"
)

var (
	count   int
	timeout time.Duration

	url       string
	headerKey string
	headerVal string
	msg       string

	caFile string
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
	flag.StringVar(&caFile, "ca", "/cert.crt", "CA root cert file")
	flag.StringVar(&msg, "msg", "HelloWorld", "message to send (for websockets)")
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

func makeWebSocketRequest(client *websocket.Dialer) func(int) func() error {
	return func(i int) func() error {
		return func() error {
			req := make(http.Header)

			log.Printf("[%d] Url=%s\n", i, url)
			if headerKey == hostKey {
				req.Add("Host", headerVal)
				log.Printf("[%d] Host=%s\n", i, headerVal)
			} else if headerKey != "" {
				req.Add(headerKey, headerVal)
				log.Printf("[%d] Header=%s:%s\n", i, headerKey, headerVal)
			}

			if msg != "" {
				log.Printf("[%d] Body=%s\n", i, msg)
			}

			conn, _, err := client.Dial(url, req)
			if err != nil {
				// timeout or bad handshake
				return err
			}
			// nolint: errcheck
			defer conn.Close()

			err = conn.WriteMessage(websocket.TextMessage, []byte(msg))
			if err != nil {
				return err
			}

			_, resp, err := conn.ReadMessage()
			if err != nil {
				return err
			}

			for _, line := range strings.Split(string(resp), "\n") {
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
	} else if strings.HasPrefix(url, "grpc://") || strings.HasPrefix(url, "grpcs://") {
		secure := strings.HasPrefix(url, "grpcs://")
		var address string
		if secure {
			address = url[len("grpcs://"):]
		} else {
			address = url[len("grpc://"):]
		}

		// grpc-go sets incorrect authority header
		authority := address
		if headerKey == hostKey {
			authority = headerVal
		}

		// transport security
		security := grpc.WithInsecure()
		if secure {
			creds, err := credentials.NewClientTLSFromFile(caFile, authority)
			if err != nil {
				log.Fatalf("failed to load client certs %s %v", caFile, err)
			}
			security = grpc.WithTransportCredentials(creds)
		}

		conn, err := grpc.Dial(address,
			security,
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
	} else if strings.HasPrefix(url, "ws://") || strings.HasPrefix(url, "wss://") {
		/* #nosec */
		client := &websocket.Dialer{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			HandshakeTimeout: timeout,
		}
		f = makeWebSocketRequest(client)
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
