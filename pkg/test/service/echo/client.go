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

package echo

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang/sync/errgroup"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"istio.io/istio/pkg/test/service/echo/proto"
)

const (
	hostKey = "Host"
)

// batchOptions provides options to the batch processor.
type batchOptions struct {
	Count     int
	QPS       int
	Timeout   time.Duration
	URL       string
	HeaderKey string
	HeaderVal string
	Message   string
	CAFile    string
}

// batch processes a batch of requests.
type batch struct {
	options batchOptions
	client  client
}

// run runs the batch and collects the results.
func (b *batch) run() ([]string, error) {
	g, _ := errgroup.WithContext(context.Background())
	responses := make([]string, b.options.Count)

	var throttle <-chan time.Time

	if b.options.QPS > 0 {
		sleepTime := time.Second / time.Duration(b.options.QPS)
		log.Printf("Sleeping %v between requests\n", sleepTime)
		throttle = time.Tick(sleepTime)
	}

	for i := 0; i < b.options.Count; i++ {
		r := request{
			RequestID: i,
			URL:       b.options.URL,
			Message:   b.options.Message,
			HeaderKey: b.options.HeaderKey,
			HeaderVal: b.options.HeaderVal,
		}
		r.RequestID = i

		if b.options.QPS > 0 {
			<-throttle
		}

		respIndex := i
		g.Go(func() error {
			resp, err := b.client.makeRequest(&r)
			if err != nil {
				return err
			}
			responses[respIndex] = string(resp)
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}
	return responses, nil
}

// stop terminates the batch processor.
func (b *batch) stop() error {
	if b.client != nil {
		return b.client.stop()
	}
	return nil
}

// newBatch creates a new batch processor with the given options.
func newBatch(ops batchOptions) (*batch, error) {
	client, err := newClient(ops)
	if err != nil {
		return nil, err
	}

	return &batch{
		client:  client,
		options: ops,
	}, nil
}

type request struct {
	URL       string
	HeaderKey string
	HeaderVal string
	RequestID int
	Message   string
}

type response string

type client interface {
	makeRequest(req *request) (response, error)
	stop() error
}

type httpClient struct {
	client *http.Client
}

func (c *httpClient) makeRequest(req *request) (response, error) {
	httpReq, err := http.NewRequest("GET", req.URL, nil)
	if err != nil {
		return "", err
	}

	var outBuffer bytes.Buffer

	outBuffer.WriteString(fmt.Sprintf("[%d] Url=%s\n", req.RequestID, req.URL))
	if req.HeaderKey == hostKey {
		httpReq.Host = req.HeaderVal
		outBuffer.WriteString(fmt.Sprintf("[%d] Host=%s\n", req.RequestID, req.HeaderVal))
	} else if req.HeaderKey != "" {
		httpReq.Header.Add(req.HeaderKey, req.HeaderVal)
		outBuffer.WriteString(fmt.Sprintf("[%d] Header=%s:%s\n", req.RequestID, req.HeaderKey, req.HeaderVal))
	}

	httpResp, err := c.client.Do(httpReq)
	if err != nil {
		return "", err
	}

	outBuffer.WriteString(fmt.Sprintf("[%d] StatusCode=%d\n", req.RequestID, httpResp.StatusCode))

	data, err := ioutil.ReadAll(httpResp.Body)
	defer func() {
		if err = httpResp.Body.Close(); err != nil {
			outBuffer.WriteString(fmt.Sprintf("[%d error] %s\n", req.RequestID, err))
		}
	}()

	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			outBuffer.WriteString(fmt.Sprintf("[%d body] %s\n", req.RequestID, line))
		}
	}

	return response(outBuffer.String()), nil
}

func (c *httpClient) stop() error {
	return nil
}

type grpcClient struct {
	conn   *grpc.ClientConn
	client proto.EchoTestServiceClient
}

func (c *grpcClient) makeRequest(req *request) (response, error) {
	grpcReq := &proto.EchoRequest{Message: fmt.Sprintf("request #%d", req.RequestID)}
	log.Printf("[%d] grpcecho.Echo(%v)\n", req.RequestID, req)
	resp, err := c.client.Echo(context.Background(), grpcReq)
	if err != nil {
		return "", err
	}

	// when the underlying HTTP2 request returns status 404, GRPC
	// request does not return an error in grpc-go.
	// instead it just returns an empty response
	var outBuffer bytes.Buffer
	for _, line := range strings.Split(resp.GetMessage(), "\n") {
		if line != "" {
			log.Printf("[%d body] %s\n", req.RequestID, line)
		}
	}
	return response(outBuffer.String()), nil
}

func (c *grpcClient) stop() error {
	return c.conn.Close()
}

type websocketClient struct {
	dialer *websocket.Dialer
}

func (c *websocketClient) makeRequest(req *request) (response, error) {
	wsReq := make(http.Header)

	var outBuffer bytes.Buffer
	outBuffer.WriteString(fmt.Sprintf("[%d] Url=%s\n", req.RequestID, req.URL))
	if req.HeaderKey == hostKey {
		wsReq.Add("Host", req.HeaderVal)
		outBuffer.WriteString(fmt.Sprintf("[%d] Host=%s\n", req.RequestID, req.HeaderVal))
	} else if req.HeaderKey != "" {
		wsReq.Add(req.HeaderKey, req.HeaderVal)
		outBuffer.WriteString(fmt.Sprintf("[%d] Header=%s:%s\n", req.RequestID, req.HeaderKey, req.HeaderVal))
	}

	if req.Message != "" {
		outBuffer.WriteString(fmt.Sprintf("[%d] Body=%s\n", req.RequestID, req.Message))
	}

	conn, _, err := c.dialer.Dial(req.URL, wsReq)
	if err != nil {
		// timeout or bad handshake
		return "", err
	}
	// nolint: errcheck
	defer conn.Close()

	err = conn.WriteMessage(websocket.TextMessage, []byte(req.Message))
	if err != nil {
		return "", err
	}

	_, resp, err := conn.ReadMessage()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(resp), "\n") {
		if line != "" {
			outBuffer.WriteString(fmt.Sprintf("[%d body] %s\n", req.RequestID, line))
		}
	}

	return response(outBuffer.String()), nil
}

func (c *websocketClient) stop() error {
	return nil
}

func newClient(ops batchOptions) (client, error) {
	if strings.HasPrefix(ops.URL, "http://") || strings.HasPrefix(ops.URL, "https://") {
		/* #nosec */
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
			Timeout: ops.Timeout,
		}
		return &httpClient{
			client: client,
		}, nil
	} else if strings.HasPrefix(ops.URL, "grpc://") || strings.HasPrefix(ops.URL, "grpcs://") {
		secure := strings.HasPrefix(ops.URL, "grpcs://")
		var address string
		if secure {
			address = ops.URL[len("grpcs://"):]
		} else {
			address = ops.URL[len("grpc://"):]
		}

		// grpc-go sets incorrect authority header
		authority := address
		if ops.HeaderKey == hostKey {
			authority = ops.HeaderVal
		}

		// transport security
		security := grpc.WithInsecure()
		if secure {
			creds, err := credentials.NewClientTLSFromFile(ops.CAFile, authority)
			if err != nil {
				log.Fatalf("failed to load client certs %s %v", ops.CAFile, err)
			}
			security = grpc.WithTransportCredentials(creds)
		}

		grpcConn, err := grpc.Dial(address,
			security,
			grpc.WithAuthority(authority),
			grpc.WithBlock(),
			grpc.WithTimeout(ops.Timeout))
		if err != nil {
			return nil, err
		}
		client := proto.NewEchoTestServiceClient(grpcConn)
		return &grpcClient{
			conn:   grpcConn,
			client: client,
		}, nil
	} else if strings.HasPrefix(ops.URL, "ws://") || strings.HasPrefix(ops.URL, "wss://") {
		/* #nosec */
		dialer := &websocket.Dialer{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			HandshakeTimeout: ops.Timeout,
		}
		return &websocketClient{
			dialer: dialer,
		}, nil
	}

	return nil, fmt.Errorf("unrecognized protocol %q", ops.URL)
}
