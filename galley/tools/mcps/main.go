package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	galley "istio.io/istio/galley/pkg/server"
)

var (
	serverAddr     = flag.String("server", "127.0.0.1:9901", "The server address")
	kubeConfig     = flag.String("kubeConfig", "", "The path to kube configuration file")
	configPath     = flag.String("configPath", "../../../tests/testdata/config", "The config files which galley will monitor")
	meshConfigFile = flag.String("meshConfigFile", "../../../istioctl/cmd/istioctl/testdata/mesh-config.yaml", "The path for mesh config")
)

func main() {
	flag.Parse()

	galleyArgs := galley.DefaultArgs()
	galleyArgs.APIAddress = *serverAddr
	if *kubeConfig == "" {
		galleyArgs.ConfigPath = *configPath
	} else {
		galleyArgs.KubeConfig = *kubeConfig
	}
	galleyArgs.Insecure = true
	galleyArgs.MeshConfigFile = *meshConfigFile
	galleyServer, err := galley.New(galleyArgs)
	if err != nil {
		fmt.Printf("Error creating galley mcp server: %v\n", err)
		os.Exit(-1)
	}
	var galleyMCPServerAddress string
	idx := strings.Index(galleyArgs.APIAddress, "://")
	if idx < 0 {
		galleyMCPServerAddress = galleyArgs.APIAddress
	} else {
		galleyMCPServerAddress = galleyArgs.APIAddress[idx+3:]
	}
	GalleyMCPServerAddress := galleyMCPServerAddress

	go galleyServer.Run()
	fmt.Printf("galley mcp server is available at: %s", GalleyMCPServerAddress)
	select {}
}
