package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/protobuf/ptypes"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/mixer/test/client/env"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/model"
	envoy "istio.io/istio/pilot/pkg/proxy/envoy/v1"
	"istio.io/istio/pilot/pkg/serviceregistry"
	agent "istio.io/istio/pkg/bootstrap"
	"istio.io/istio/tests/util"
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
	err := startPilot()
	if err != nil {
		return err
	}

	err = startMixer()
	if err != nil {
		return err
	}

	// Mixer test servers
	srv, err := env.NewHTTPServer(7070)
	if err != nil {
		return err
	}
	srv.Start()

	go util.RunHTTP(7072, "v1")
	go util.RunGRPC(7073, "v1", "", "")
	go util.RunHTTP(7074, "v2")
	go util.RunGRPC(7075, "v2", "", "")
	err = startEnvoy()
	if err != nil {
		return err
	}

	return nil
}

func startMixer() error {
	srv, err := env.NewMixerServer(9091, false)
	if err != nil {
		return err
	}
	srv.Start()

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
		DiscoveryAddress:      "localhost:8080",
		ConfigPath:            util.IstioOut,
		BinaryPath:            util.IstioBin + "/envoy",
		ServiceCluster:        "test",
		CustomConfigFile:      util.IstioSrc + "/tools/deb/envoy_bootstrap_v2.json",
		DiscoveryRefreshDelay: ptypes.DurationProto(10 * time.Second), // crash if not set
		ConnectTimeout:        ptypes.DurationProto(5 * time.Second),  // crash if not set
		DrainDuration:         ptypes.DurationProto(30 * time.Second), // crash if 0

	}
	cfgF, err := agent.WriteBootstrap(cfg, "sidecar~127.0.0.2~a~a", 1, []string{}, nil)
	if err != nil {
		return err
	}
	stop := make(chan error)
	agent.RunProxy(cfg, "node", 1, cfgF, stop, os.Stdout, os.Stderr, []string{
		"--disable-hot-restart",
	})

	return nil
}

func startPilot() error {
	stop := make(chan struct{})

	mcfg := model.DefaultMeshConfig()
	mcfg.ProxyHttpPort = 15002

	// Create a test pilot discovery service configured to watch the tempDir.
	args := bootstrap.PilotArgs{
		Namespace: "testing",
		DiscoveryOptions: envoy.DiscoveryServiceOptions{
			Port:            8080,
			GrpcAddr:        ":15010",
			EnableCaching:   true,
			EnableProfiling: true,
		},

		Mesh: bootstrap.MeshArgs{
			MixerAddress:    "localhost:9091",
			RdsRefreshDelay: ptypes.DurationProto(10 * time.Millisecond),
		},
		Config: bootstrap.ConfigArgs{
			KubeConfig: util.IstioSrc + "/.circleci/config",
		},
		Service: bootstrap.ServiceArgs{
			// Using the Mock service registry, which provides the hello and world services.
			Registries: []string{
				string(serviceregistry.MockRegistry)},
		},
		MeshConfig: &mcfg,
	}
	bootstrap.FilepathWalkInterval = 5 * time.Second
	// Static testdata, should include all configs we want to test.
	args.Config.FileDir = util.IstioSrc + "/tests/testdata/config"

	// Create and setup the controller.
	s, err := bootstrap.NewServer(args)
	if err != nil {
		return err
	}

	// Start the server.
	_, err = s.Start(stop)
	if err != nil {
		return err
	}
	return nil
}
