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

package main

import (
	"flag"
	"fmt"
	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/pkg/ctrlz"
	istiolog "istio.io/istio/pkg/log"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gogo/protobuf/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mixerEnv "istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy"
	"istio.io/istio/pilot/pkg/serviceregistry"
	agent "istio.io/istio/pkg/bootstrap"
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/tests/util"
)

var (
	runEnvoy  = flag.Bool("envoy", true, "Start envoy")
	configDir = flag.String("conf", "", "Config dir. Empty to use k8s")
)

// hyperistio runs all istio components in one binary, using a directory based config by
// default. It is intended for testing/debugging/prototyping.
func main() {
	flag.Parse()
	err := startAll()
	if err != nil {
		log.Fatal("Failed to start ", err)
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	<-sigs

	//select{}
}

func startAll() error {
	err := startGalley()
	if err != nil {
		return err
	}

	err = startPilot()
	if err != nil {
		return err
	}

	err = startMixer()
	if err != nil {
		return err
	}

	// Mixer test servers
	srv, err := mixerEnv.NewHTTPServer(7070)
	if err != nil {
		return err
	}
	errCh := srv.Start()
	if err = <-errCh; err != nil {
		log.Fatalf("backend server start failed %v", err)
	}

	go util.RunHTTP(7072, "v1")
	go util.RunGRPC(7073, "v1", "", "")
	go util.RunHTTP(7074, "v2")
	go util.RunGRPC(7075, "v2", "", "")
	if *runEnvoy {
		err = startEnvoy()
		if err != nil {
			return err
		}
	}

	return nil
}

func startMixer() error {
	srv, err := mixerEnv.NewMixerServer(9091, false, false)
	if err != nil {
		return err
	}
	errCh := srv.Start()
	if err = <-errCh; err != nil {
		log.Fatalf("mixer start failed %v", err)
	}

	go func() {
		for {
			r := srv.GetReport()
			fmt.Println("MixerReport: ", r)
		}
	}()

	return nil
}

func startEnvoy() error {
	cfg := &meshconfig.ProxyConfig{
		DiscoveryAddress: "localhost:16010",
		ConfigPath:       env.IstioOut,
		BinaryPath:       env.IstioBin + "/envoy",
		ServiceCluster:   "test",
		CustomConfigFile: env.IstioSrc + "/tools/deb/envoy_bootstrap_v2.json",
		ConnectTimeout:   types.DurationProto(5 * time.Second),  // crash if not set
		DrainDuration:    types.DurationProto(30 * time.Second), // crash if 0
		StatNameLength:   189,
	}
	nodeId := "sidecar~127.0.0.2~a.default~default.svc.cluster.local"
	cfgF, err := agent.WriteBootstrap(cfg,
		nodeId,
		1, []string{}, nil, os.Environ())
	if err != nil {
		return err
	}
	stop := make(chan error)
	envoyLog, err := os.Create(env.IstioOut + "/envoy_hyperistio_sidecar.log")
	if err != nil {
		envoyLog = os.Stderr
	}
	agent.RunProxy(cfg, nodeId, 1, cfgF, stop, envoyLog, envoyLog, []string{
		"--disable-hot-restart", // "-l", "trace",
	})

	return nil
}

// Start galley. Will use KUBECONFIG, if set - otherwise the configDir flag.
func startGalley() error {
	gs, err := server.New(&server.Args{
		ConfigPath:             *configDir,
		KubeConfig:             os.Getenv("KUBECONFIG"),
		Insecure:               true,
		EnableGRPCTracing:      true,
		APIAddress:             "tcp://0.0.0.0:9901",
		LoggingOptions:         istiolog.DefaultOptions(), // Can't be nil
		IntrospectionOptions:   ctrlz.DefaultOptions(),    // can't be nil - crash
		MaxReceivedMessageSize: 1024 * 1024 * 1024,
		MaxConcurrentStreams:   10000,
	})
	if err != nil {
		return err
	}

	go gs.Run()

	return nil
}

// startPilot with defaults:
// - http port 15007
// - grpc on 15010
// - grpcs in 15011 - certs from PILOT_CERT_DIR or ./tests/testdata/certs/pilot
// - mixer set to localhost:9091 (runs in-process),
//-  http proxy on 15002 (so tests can be run without iptables)
//- config from $ISTIO_CONFIG dir (defaults to in-source tests/testdata/config)
func startPilot() error {
	stop := make(chan struct{})

	mcfg := model.DefaultMeshConfig()
	mcfg.ProxyHttpPort = 15002

	// Create a test pilot discovery service configured to watch the tempDir.
	args := bootstrap.PilotArgs{
		Namespace: "testing",
		DiscoveryOptions: envoy.DiscoveryServiceOptions{
			HTTPAddr:        ":16007",
			GrpcAddr:        ":16010",
			SecureGrpcAddr:  ":16011",
			EnableCaching:   true,
			EnableProfiling: true,
		},

		MCPMaxMessageSize: 4 * 1024 * 1024,
		Mesh: bootstrap.MeshArgs{
			MixerAddress:    "localhost:9091",
			RdsRefreshDelay: types.DurationProto(10 * time.Millisecond),
		},
		Config: bootstrap.ConfigArgs{
			KubeConfig: env.IstioSrc + "/.circleci/config",
		},
		Service: bootstrap.ServiceArgs{
			// Using the Mock service registry, which provides the hello and world services.
			Registries: []string{
				string(serviceregistry.MockRegistry)},
		},
		MCPServerAddrs: []string{"mcp://localhost:9901"},
		MeshConfig:     &mcfg,
	}
	bootstrap.PilotCertDir = env.IstioSrc + "/tests/testdata/certs/pilot"

	bootstrap.FilepathWalkInterval = 5 * time.Second
	// Static testdata, should include all configs we want to test.
	args.Config.FileDir = os.Getenv("ISTIO_CONFIG")
	if args.Config.FileDir == "" {
		args.Config.FileDir = env.IstioSrc + "/tests/testdata/config"
	}
	log.Println("Using mock configs: ", args.Config.FileDir)
	// Create and setup the controller.
	s, err := bootstrap.NewServer(args)
	if err != nil {
		return err
	}

	// Start the server.
	if err := s.Start(stop); err != nil {
		return err
	}
	return nil
}
