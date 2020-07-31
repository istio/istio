//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package envoy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"

	"istio.io/pkg/log"
)

// Shutdown initiates a graceful shutdown of Envoy.
func Shutdown(adminPort uint32) error {
	_, err := doEnvoyPost("quitquitquit", "", "", adminPort)
	return err
}

// DrainListeners drains inbound listeners of Envoy so that inflight requests
// can gracefully finish and even continue making outbound calls as needed.
func DrainListeners(adminPort uint32, inboundonly bool) error {
	var drainURL string
	if inboundonly {
		drainURL = "drain_listeners?inboundonly"
	} else {
		drainURL = "drain_listeners"
	}
	res, err := doEnvoyPost(drainURL, "", "", adminPort)
	log.Debugf("Drain listener endpoint response : %s", res.String())
	return err
}

// GetServerInfo returns a structure representing a call to /server_info
func GetServerInfo(adminPort uint32) (*envoyAdmin.ServerInfo, error) {
	buffer, err := doEnvoyGet("server_info", adminPort)
	if err != nil {
		return nil, err
	}

	msg := &envoyAdmin.ServerInfo{}
	if err := unmarshal(buffer.String(), msg); err != nil {
		return nil, err
	}

	return msg, nil
}

// GetConfigDump polls Envoy admin port for the config dump and returns the response.
func GetConfigDump(adminPort uint32) (*envoyAdmin.ConfigDump, error) {
	buffer, err := doEnvoyGet("config_dump", adminPort)
	if err != nil {
		return nil, err
	}

	msg := &envoyAdmin.ConfigDump{}
	if err := unmarshal(buffer.String(), msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func doEnvoyGet(path string, adminPort uint32) (*bytes.Buffer, error) {
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/%s", adminPort, path)
	buffer, err := doHTTPGet(requestURL)
	if err != nil {
		return nil, err
	}
	return buffer, nil
}

func doEnvoyPost(path, contentType, body string, adminPort uint32) (*bytes.Buffer, error) {
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/%s", adminPort, path)
	buffer, err := doHTTPPost(requestURL, contentType, body)
	if err != nil {
		return nil, err
	}
	return buffer, nil
}

func doHTTPGet(requestURL string) (*bytes.Buffer, error) {
	response, err := http.Get(requestURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status %d", response.StatusCode)
	}

	var b bytes.Buffer
	if _, err := io.Copy(&b, response.Body); err != nil {
		return nil, err
	}
	return &b, nil
}

func doHTTPPost(requestURL, contentType, body string) (*bytes.Buffer, error) {
	response, err := http.Post(requestURL, contentType, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	var b bytes.Buffer
	if _, err := io.Copy(&b, response.Body); err != nil {
		return nil, err
	}
	return &b, nil
}

func unmarshal(jsonString string, msg proto.Message) error {
	u := jsonpb.Unmarshaler{
		AllowUnknownFields: true,
	}
	return u.Unmarshal(strings.NewReader(jsonString), msg)
}
