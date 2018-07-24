//  Copyright 2018 Istio Authors
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
	"strconv"
	"strings"
	"time"

	envoy_admin_v2alpha "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	"github.com/gogo/protobuf/jsonpb"
)

// HealthCheckState represents a health checking state returned from /server_info
type HealthCheckState string

const (
	// HealthCheckLive indicates Envoy is live and ready to serve requests
	HealthCheckLive HealthCheckState = "live"
	// HealthCheckDraining indicates Envoy is not currently capable of serving requests
	HealthCheckDraining HealthCheckState = "draining"
)

const (
	healthCheckTimeout  = 10 * time.Second
	healthCheckInterval = 100 * time.Millisecond
)

var (
	nilServerInfo = ServerInfo{}
)

// ServerInfo is the result of a request to /server_info
type ServerInfo struct {
	ProcessName                  string
	CompiledSHABuildType         string
	HealthCheckState             HealthCheckState
	CurrentHotRestartEpochUptime time.Duration
	TotalUptime                  time.Duration
	CurrentHotRestartEpoch       int
}

// GetServerInfo returns a structure representing a call to /server_info
func GetServerInfo(adminPort int) (ServerInfo, error) {
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/server_info", adminPort)
	buffer, err := doHTTPGet(requestURL)
	if err != nil {
		return nilServerInfo, err
	}
	body := strings.TrimSpace(buffer.String())

	parts := strings.Split(body, " ")
	if len(parts) != 6 {
		return nilServerInfo, fmt.Errorf("call to /server_info returned invalid response: %s", body)
	}

	currentHotRestartEpochUptime, err := strconv.Atoi(parts[3])
	if err != nil {
		return nilServerInfo, err
	}

	totalUptime, err := strconv.Atoi(parts[4])
	if err != nil {
		return nilServerInfo, err
	}

	currentHotRestartEpoch, err := strconv.Atoi(parts[5])
	if err != nil {
		return nilServerInfo, err
	}

	return ServerInfo{
		ProcessName:                  parts[0],
		CompiledSHABuildType:         parts[1],
		HealthCheckState:             HealthCheckState(parts[2]),
		CurrentHotRestartEpochUptime: time.Second * time.Duration(currentHotRestartEpochUptime),
		TotalUptime:                  time.Second * time.Duration(totalUptime),
		CurrentHotRestartEpoch:       currentHotRestartEpoch,
	}, nil
}

// WaitForHealthCheckLive polls the server info for Envoy and waits for it to transition to "live".
func WaitForHealthCheckLive(adminPort int) error {
	endTime := time.Now().Add(healthCheckTimeout)
	for {
		var info ServerInfo
		info, err := GetServerInfo(adminPort)
		if err == nil {
			if info.HealthCheckState == HealthCheckLive {
				// It's running, we can return now.
				return nil
			}
		}

		// Stop trying after the timeout
		if time.Now().After(endTime) {
			err = fmt.Errorf("failed to start envoy after %ds. Error: %v", healthCheckTimeout/time.Second, err)
			return err
		}

		// Sleep a short before retry.
		time.Sleep(healthCheckInterval)
	}
}

// GetConfigDumpStr polls Envoy admin port for the config dump and returns the response as a string.
// TODO(nmittler): We shouldn't need this method. Look into how to properly marshal the config dump proto.
func GetConfigDumpStr(adminPort int) (string, error) {
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/config_dump", adminPort)
	buffer, err := doHTTPGet(requestURL)
	if err != nil {
		return "", err
	}
	return buffer.String(), nil
}

// GetConfigDump polls Envoy admin port for the config dump and returns the response.
func GetConfigDump(adminPort int) (*envoy_admin_v2alpha.ConfigDump, error) {
	requestURL := fmt.Sprintf("http://127.0.0.1:%d/config_dump", adminPort)
	buffer, err := doHTTPGet(requestURL)
	if err != nil {
		return nil, err
	}

	msg := &envoy_admin_v2alpha.ConfigDump{}
	if err := jsonpb.Unmarshal(buffer, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func doHTTPGet(requestURL string) (*bytes.Buffer, error) {
	response, err := http.Get(requestURL)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status %d", response.StatusCode)
	}

	var b bytes.Buffer
	if _, err := io.Copy(&b, response.Body); err != nil {
		return nil, err
	}
	return &b, nil
}
