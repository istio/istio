package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"

	"github.com/envoyproxy/go-control-plane/pkg/cache"
	xds "github.com/envoyproxy/go-control-plane/pkg/grpc"
	"github.com/golang/glog"
	"google.golang.org/grpc"
	"istio.io/istio/pilot/cmd"
	"istio.io/istio/pilot/proxy/envoy/v2"
)

func main() {
	flag.Parse()
	stop := make(chan struct{})

	if id == "" {
		if name, exists := os.LookupEnv("POD_NAME"); exists {
			id = name
		}
		if namespace, exists := os.LookupEnv("POD_NAMESPACE"); exists {
			id = namespace + "/" + id
		}
	}

	config := cache.NewSimpleCache(v2.Hasher{}, nil /* TODO */)
	server := xds.NewServer(config)
	grpcServer := grpc.NewServer()
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		glog.Fatalf("failed to listen: %v", err)
	}
	server.Register(grpcServer)

	go func() {
		if err = grpcServer.Serve(lis); err != nil {
			glog.Error(err)
		}
	}()

	// run envoy process
	envoy := exec.Command(binPath,
		"-c", "bootstrap.json",
		"--drain-time-s", "1")
	envoy.Stdout = os.Stdout
	envoy.Stderr = os.Stderr
	envoy.Start()

	var generator *v2.Generator
	if validate {
		generator = &v2.Generator{Cache: config, ID: id, Path: "testdata"}
		generator.Generate()
	} else {
		generator, err = v2.NewGenerator(config, configPath, id, kubeconfig)
		if err != nil {
			glog.Fatal(err)
		}
		generator.Run(stop)
	}

	// expose profiling endpoint
	http.ListenAndServe(":15005", nil)

	cmd.WaitSignal(stop)
}

var (
	kubeconfig string
	id         string
	namespace  string
	binPath    string
	configPath string
	port       int

	validate bool
)

func init() {
	flag.StringVar(&kubeconfig, "kubeconfig", "",
		"Use a Kubernetes configuration file instead of in-cluster configuration")
	flag.StringVar(&id, "id", "", "Workload ID (e.g. pod namespace/name)")
	flag.IntVar(&port, "port", 15003,
		"ADS port")
	flag.BoolVar(&validate, "valid", false,
		"Validate only (for testing and debugging)")
	flag.StringVar(&binPath, "bin", "/usr/local/bin/envoy", "Envoy binary")
	flag.StringVar(&configPath, "config", ".", "Config file path")
}
