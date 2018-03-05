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

package util

import (
	"fmt"
	"net"
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes"
	envoy "istio.io/istio/pilot/pkg/proxy/envoy/v1"

	"istio.io/istio/pilot/pkg/model"
)

// Extracted from watcher_test, will run envoy
var (
	IstioTop = os.Getenv("TOP")
	IstioSrc = os.Getenv("ISTIO_GO")
	IstioBin = os.Getenv("ISTIO_BIN")
	IstioOut = os.Getenv("ISTIO_OUT")
)

func init() {
	if IstioTop == "" {
		// Assume it is run inside istio.io/istio
		current, _ := os.Getwd()
		idx := strings.Index(current, "/src/istio.io/istio")
		if idx > 0 {
			IstioTop = current[0:idx]
		}
	}
	if IstioSrc == "" {
		IstioSrc = IstioTop + "/src/istio.io/istio"
	}
	if IstioOut == "" {
		IstioOut = IstioTop + "/out"
	}
	if IstioBin == "" {
		IstioBin = IstioTop + "/out/" + runtime.GOOS + "_" + runtime.GOARCH + "/release"
	}
}

// RunEnvoy runs an envoy process. Base is the basename for the generate envoy.json, template
// is the template to be used to run envoy.
func RunEnvoy(base string, template string) error {
	pilot := EnsureTestServer()
	config := model.DefaultProxyConfig()

	// Envoy's path
	config.BinaryPath = path.Join(IstioBin, "envoy")

	var err error

	// Where to write the temp config
	config.ConfigPath = IstioOut
	config.DiscoveryRefreshDelay = ptypes.DurationProto(10000 * time.Millisecond)
	// Template to use
	config.ProxyBootstrapTemplatePath = IstioSrc + "/" + template

	_, port, err := net.SplitHostPort(pilot.HTTPListeningAddr.String())
	config.DiscoveryAddress = "localhost:" + port
	_, grpcPort, err := net.SplitHostPort(pilot.GRPCListeningAddr.String())

	done := make(chan error, 1)

	envoyProxy := envoy.NewV2ProxyCustom(config, "router~x~x~x", "info", []string{
		"spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account"},
		map[string]interface{}{
			"pilot_grpc": "localhost:" + grpcPort,
		}, done)

	abortCh := make(chan error, 1)

	cfg := &envoy.Config{}
	// done channel will get termination events.
	if err = envoyProxy.Run(cfg, 0, abortCh); err != nil {
		fmt.Println("Failed to start envoy", err)
	}

	return nil
}
