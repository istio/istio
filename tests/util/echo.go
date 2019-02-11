// Copyright 2018 Istio Authors
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

package util

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	echopb "istio.io/istio/pkg/test/application/echo/proto"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// allow all connections by default
		return true
	},
} //defaults

type pilotTestHandler struct {
	port    int
	version string
}

// Imagine a pie of different flavors.
// The flavors are the HTTP response codes.
// The chance of a particular flavor is ( slices / sum of slices ).
type codeAndSlices struct {
	httpResponseCode int
	slices           int
}

func (h *pilotTestHandler) addResponsePayload(r *http.Request, body *bytes.Buffer) { // nolint:interfacer

	body.WriteString("ServiceVersion=" + h.version + "\n")
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
}

func (h *pilotTestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("testwebsocket") != "" {
		h.WebSocketEcho(w, r)
		return
	}

	body := bytes.Buffer{}

	if err := r.ParseForm(); err != nil {
		body.WriteString("ParseForm() error: " + err.Error() + "\n")
	}

	if len(r.FormValue("url")) > 0 {
		echoClientHandler(w, r)
		return
	}

	// If the request has form ?codes=code[:chance][,code[:chance]]* return those codes, rather than 200
	// For example, ?codes=500:1,200:1 returns 500 1/2 times and 200 1/2 times
	// For example, ?codes=500:90,200:10 returns 500 90% of times and 200 10% of times
	if err := setResponseFromCodes(r, w); err != nil {
		body.WriteString("codes error: " + err.Error() + "\n")
	}

	h.addResponsePayload(r, &body)

	w.Header().Set("Content-Type", "application/text")
	if _, err := w.Write(body.Bytes()); err != nil {
		log.Println(err.Error())
	}
}

func (h *pilotTestHandler) Echo(ctx context.Context, req *echopb.EchoRequest) (*echopb.EchoResponse, error) {
	body := bytes.Buffer{}
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		for key, vals := range md {
			body.WriteString(key + "=" + strings.Join(vals, " ") + "\n")
		}
	}
	body.WriteString("ServiceVersion=" + h.version + "\n")
	body.WriteString("ServicePort=" + strconv.Itoa(h.port) + "\n")
	body.WriteString("Echo=" + req.GetMessage())
	return &echopb.EchoResponse{Message: body.String()}, nil
}

func (h *pilotTestHandler) ForwardEcho(ctx context.Context, in *echopb.ForwardEchoRequest) (*echopb.ForwardEchoResponse, error) {
	return nil, fmt.Errorf("unsupported operation")
}

func (h *pilotTestHandler) WebSocketEcho(w http.ResponseWriter, r *http.Request) {
	body := bytes.Buffer{}
	h.addResponsePayload(r, &body) // create resp payload apriori

	// adapted from https://github.com/gorilla/websocket/blob/master/examples/echo/server.go
	// First send upgrade headers
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("websocket-echo upgrade failed:", err)
		return
	}

	// nolint: errcheck
	defer c.Close()

	// ping
	mt, message, err := c.ReadMessage()
	if err != nil {
		log.Println("websocket-echo read failed:", err)
		return
	}

	// pong
	body.Write(message)
	err = c.WriteMessage(mt, body.Bytes())
	if err != nil {
		log.Println("websocket-echo write failed:", err)
		return
	}
}

// RunHTTP runs the http echo service.
func RunHTTP(port int, version string) {
	fmt.Printf("Listening HTTP1.1 on %v\n", port)
	h := &pilotTestHandler{port: port, version: version}
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), h); err != nil {
		log.Println(err.Error())
	}
}

// RunGRPC runs the GRPC service, with optional TLS
func RunGRPC(port int, version, crt, key string) {
	fmt.Printf("Listening GRPC on %v\n", port)
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	h := pilotTestHandler{port: port, version: version}

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
	echopb.RegisterEchoTestServiceServer(grpcServer, &h)
	if err = grpcServer.Serve(lis); err != nil {
		log.Println(err.Error())
	}
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

// Client side
type job func(int, io.Writer) error

const (
	hostKey = "Host"
)

// EchoClient controls the client
type EchoClient struct {
	count     int
	timeout   time.Duration
	qps       int
	url       string
	headerKey string
	headerVal string
	msg       string

	caFile   string
	grpcConn *grpc.ClientConn

	httpClient *http.Client
	wsClient   *websocket.Dialer
	grpcClient echopb.EchoTestServiceClient
}

func (ec *EchoClient) makeHTTPRequest(i int, w io.Writer) error {
	req, err := http.NewRequest("GET", ec.url, nil)
	if err != nil {
		return err
	}

	//log.Printf("[%d] Url=%s\n", i, ec.url)
	if ec.headerKey == hostKey {
		req.Host = ec.headerVal
		//log.Printf("[%d] Host=%s\n", i, ec.headerVal)
	} else if ec.headerKey != "" {
		req.Header.Add(ec.headerKey, ec.headerVal)
		//log.Printf("[%d] Header=%s:%s\n", i, ec.headerKey, ec.headerVal)
	}

	resp, err := ec.httpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != 200 && w != nil {
		fmt.Fprintf(w, "[%d] StatusCode=%d\n", i, resp.StatusCode)
	}

	data, err := ioutil.ReadAll(resp.Body)
	_ = resp.Body.Close()

	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			if w != nil {
				fmt.Fprintf(w, "[%d body] %s\n", i, line)
			}
		}
	}

	return nil
}

func (ec *EchoClient) makeWebSocketRequest(i int, w io.Writer) error {
	req := make(http.Header)

	//log.Printf("[%d] Url=%s\n", i, ec.url)
	if ec.headerKey == hostKey {
		req.Add("Host", ec.headerVal)
		//log.Printf("[%d] Host=%s\n", i, ec.headerVal)
	} else if ec.headerKey != "" {
		req.Add(ec.headerKey, ec.headerVal)
		//log.Printf("[%d] Header=%s:%s\n", i, ec.headerKey, ec.headerVal)
	}

	//if ec.msg != "" {
	//	log.Printf("[%d] Body=%s\n", i, ec.msg)
	//}

	conn, _, err := ec.wsClient.Dial(ec.url, req)
	if err != nil {
		// timeout or bad handshake
		return err
	}
	// nolint: errcheck
	defer conn.Close()

	err = conn.WriteMessage(websocket.TextMessage, []byte(ec.msg))
	if err != nil {
		return err
	}

	_, resp, err := conn.ReadMessage()
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(resp), "\n") {
		if line != "" {
			if w != nil {
				fmt.Fprintf(w, "[%d body] %s\n", i, line)
			}
		}
	}

	return nil
}

func (ec *EchoClient) makeGRPCRequest(i int, w io.Writer) error {
	req := &echopb.EchoRequest{Message: fmt.Sprintf("request #%d", i)}
	//log.Printf("[%d] grpcecho.Echo(%v)\n", i, req)
	resp, err := ec.grpcClient.Echo(context.Background(), req)
	if err != nil {
		return err
	}

	// when the underlying HTTP2 request returns status 404, GRPC
	// request does not return an error in grpc-go.
	// instead it just returns an empty response
	for _, line := range strings.Split(resp.GetMessage(), "\n") {
		if line != "" {
			if w != nil {
				fmt.Fprintf(w, "[%d body] %s\n", i, line)
			}
		}
	}
	return nil
}

// Initialize the http/ws/grpc client and return the function to call.
func (ec *EchoClient) setupDefaultTest() (job, error) {
	if strings.HasPrefix(ec.url, "http://") || strings.HasPrefix(ec.url, "https://") {
		/* #nosec */
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
			Timeout: ec.timeout,
		}
		ec.httpClient = client
		return ec.makeHTTPRequest, nil
	} else if strings.HasPrefix(ec.url, "grpc://") || strings.HasPrefix(ec.url, "grpcs://") {
		secure := strings.HasPrefix(ec.url, "grpcs://")
		var address string
		if secure {
			address = ec.url[len("grpcs://"):]
		} else {
			address = ec.url[len("grpc://"):]
		}

		// grpc-go sets incorrect authority header
		authority := address
		if ec.headerKey == hostKey {
			authority = ec.headerVal
		}

		// transport security
		security := grpc.WithInsecure()
		if secure {
			creds, err := credentials.NewClientTLSFromFile(ec.caFile, authority)
			if err != nil {
				return nil, err
			}
			security = grpc.WithTransportCredentials(creds)
		}

		var err error
		ctx, cancel := context.WithTimeout(context.Background(), ec.timeout)
		defer cancel()

		ec.grpcConn, err = grpc.DialContext(ctx, address,
			security,
			grpc.WithAuthority(authority),
			grpc.WithBlock())
		if err != nil {
			return nil, err
		}
		client := echopb.NewEchoTestServiceClient(ec.grpcConn)
		ec.grpcClient = client
		return ec.makeGRPCRequest, nil
	} else if strings.HasPrefix(ec.url, "ws://") || strings.HasPrefix(ec.url, "wss://") {
		/* #nosec */
		client := &websocket.Dialer{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			HandshakeTimeout: ec.timeout,
		}
		ec.wsClient = client
		return ec.makeWebSocketRequest, nil
	}

	return nil, fmt.Errorf("invalid URL %s", ec.url)
}

func echoClientHandler(w http.ResponseWriter, r *http.Request) {

	//flag.IntVar(&count, "count", 1, "Number of times to make the request")
	//flag.IntVar(&qps, "qps", 0, "Queries per second")
	//flag.DurationVar(&timeout, "timeout", 15*time.Second, "Request timeout")
	//flag.StringVar(&url, "url", "", "Specify URL")
	//flag.StringVar(&headerKey, "key", "", "Header key (use Host for authority)")
	//flag.StrinproxyUrlgVar(&headerVal, "val", "", "Header value")
	//flag.StringVar(&caFile, "ca", "/cert.crt", "CA root cert file")
	//flag.StringVar(&msg, "msg", "HelloWorld", "message to send (for websockets)")

	r.ParseForm()

	// ca not yet supported

	ec := &EchoClient{
		url:       r.FormValue("url"),
		headerKey: r.FormValue("key"),
		headerVal: r.FormValue("val"),
		msg:       r.FormValue("msg"),
		timeout:   15 * time.Second,
	}

	qpsS := r.FormValue("qps")
	if len(qpsS) > 0 {
		ec.qps, _ = strconv.Atoi(qpsS)
	}
	countS := r.FormValue("count")
	if len(qpsS) > 0 {
		ec.count, _ = strconv.Atoi(countS)
	}

	j, err := ec.setupDefaultTest()
	if j == nil {
		fmt.Fprintf(w, "Error: %v", err)
		w.WriteHeader(501)
		return
	}
	defer func() {
		if ec.grpcConn != nil {
			if err1 := ec.grpcConn.Close(); err1 != nil {
				log.Println(err1)
			}
		}
	}()

	if ec.qps == 0 {
		// one request
		err = j(0, w)
	} else {

		sleepTime := time.Second / time.Duration(ec.qps)
		//log.Printf("Sleeping %v between requests\n", sleepTime)
		throttle := time.Tick(sleepTime)

		var wg sync.WaitGroup
		var m sync.Mutex

		for i := 0; i < ec.count; i++ {
			<-throttle
			wg.Add(1)

			ic := i
			go func() {
				defer wg.Done()
				e := j(ic, w)
				if e != nil {
					m.Lock()
					err = e
					m.Unlock()
				}
			}()
		}
		wg.Wait()
	}

	if err != nil {
		fmt.Fprintf(w, "Error %s\n", err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(200)
}
