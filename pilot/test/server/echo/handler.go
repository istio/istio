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

package echo

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
	"google.golang.org/grpc/metadata"

	pb "istio.io/istio/pilot/test/grpcecho"
	"istio.io/istio/pkg/log"
)

type handler struct {
	port    int
	version string
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// allow all connections by default
		return true
	},
} //defaults

// Imagine a pie of different flavors.
// The flavors are the HTTP response codes.
// The chance of a particular flavor is ( slices / sum of slices ).
type codeAndSlices struct {
	httpResponseCode int
	slices           int
}

func (h handler) addResponsePayload(r *http.Request, body *bytes.Buffer) {

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

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("testwebsocket") != "" {
		h.WebSocketEcho(w, r)
		return
	}

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

	h.addResponsePayload(r, &body)

	w.Header().Set("Content-Type", "application/text")
	if _, err := w.Write(body.Bytes()); err != nil {
		log.Warna(err)
	}
}

func (h handler) Echo(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
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
	return &pb.EchoResponse{Message: body.String()}, nil
}

func (h handler) WebSocketEcho(w http.ResponseWriter, r *http.Request) {
	body := bytes.Buffer{}
	h.addResponsePayload(r, &body) // create resp payload apriori

	// adapted from https://github.com/gorilla/websocket/blob/master/examples/echo/server.go
	// First send upgrade headers
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Warna("websocket-echo upgrade failed:", err)
		return
	}

	// nolint: errcheck
	defer c.Close()

	// ping
	mt, message, err := c.ReadMessage()
	if err != nil {
		log.Warna("websocket-echo read failed:", err)
		return
	}

	// pong
	body.Write(message)
	err = c.WriteMessage(mt, body.Bytes())
	if err != nil {
		log.Warna("websocket-echo write failed:", err)
		return
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
