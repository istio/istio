// Copyright 2016 Google Inc.
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
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/golang/glog"
	"github.com/spf13/cobra"

	"istio.io/mixer/adapters"
	"istio.io/mixer/adapters/factMapper"
)

type serverArgs struct {
	port                 uint
	maxMessageSize       uint
	maxConcurrentStreams uint
	compressedPayload    bool
	serverCertFile       string
	serverKeyFile        string
	clientCertFiles      string
}

func printBuilders(listName string, builders map[string]adapters.Builder) {
	fmt.Printf("%s\n", listName)
	if len(builders) > 0 {
		for _, b := range builders {
			fmt.Printf("  %s: %s\n", b.Name(), b.Description())
		}
	} else {
		fmt.Printf("  <none>\n")
	}

	fmt.Printf("\n")
}

func listBuilders() error {
	mgr, err := NewAdapterManager()
	if err != nil {
		return fmt.Errorf("Unable to initialize adapters: %v", err)
	}

	printBuilders("Fact Converters", mgr.FactConverters)
	printBuilders("Fact Updaters", mgr.FactUpdaters)
	printBuilders("List Checkers", mgr.ListCheckers)

	return nil
}

func runServer(sa *serverArgs) error {
	var err error
	var serverCert *tls.Certificate
	var clientCerts *x509.CertPool

	if sa.serverCertFile != "" && sa.serverKeyFile != "" {
		sc, err := tls.LoadX509KeyPair(sa.serverCertFile, sa.serverKeyFile)
		if err != nil {
			return fmt.Errorf("Failed to load server certificate and server key: %v", err)
		}
		serverCert = &sc
	}

	if sa.clientCertFiles != "" {
		clientCerts = x509.NewCertPool()
		for _, clientCertFile := range strings.Split(sa.clientCertFiles, ",") {
			pem, err := ioutil.ReadFile(clientCertFile)
			if err != nil {
				return fmt.Errorf("Failed to load client certificate: %v", err)
			}
			clientCerts.AppendCertsFromPEM(pem)
		}
	}

	var adapterMgr *AdapterManager
	if adapterMgr, err = NewAdapterManager(); err != nil {
		return fmt.Errorf("Unable to initialize adapters: %v", err)
	}

	// TODO: hackily create a fact mapper builder & adapter.
	// This necessarily needs to be discovered & created through normal
	// adapter config goo, but that doesn't exist yet
	rules := make(map[string]string)
	rules["Lab1"] = "Fact1|Fact2"
	builder := adapterMgr.FactConverters["FactMapper"]
	var adapter adapters.Adapter
	adapter, err = builder.NewAdapter(&factMapper.AdapterConfig{Rules: rules})
	if err != nil {
		return fmt.Errorf("Unable to create fact conversion adapter " + err.Error())
	}
	factConverter := adapter.(adapters.FactConverter)

	apiServerOptions := APIServerOptions{
		Port:                 uint16(sa.port),
		MaxMessageSize:       sa.maxMessageSize,
		MaxConcurrentStreams: sa.maxConcurrentStreams,
		CompressedPayload:    sa.compressedPayload,
		ServerCertificate:    serverCert,
		ClientCertificates:   clientCerts,
		Handlers:             NewAPIHandlers(),
		FactConverter:        factConverter,
	}

	var apiServer *APIServer
	if apiServer, err = NewAPIServer(&apiServerOptions); err != nil {
		return fmt.Errorf("Unable to initialize API server " + err.Error())
	}
	apiServer.Start()
	return nil
}

// withArgs is like main except that it is parameterized with the
// command-line arguments to use, along with a function to call
// in case of errors. This allows the function to be invoked
// from test code.
func withArgs(args []string, errorf func(format string, a ...interface{})) {

	rootCmd := cobra.Command{
		Use:   "mixer",
		Short: "The Istio mixer provides control plane functionality to the Istio proxy and services",
	}
	rootCmd.SetArgs(args)

	adapterCmd := cobra.Command{
		Use:   "adapter",
		Short: "Diagnostics for the available mixer adapters",
	}
	rootCmd.AddCommand(&adapterCmd)
	rootCmd.Flags().AddGoFlagSet(flag.CommandLine)

	listCmd := cobra.Command{
		Use:   "list",
		Short: "List available adapter builders",
		Run: func(cmd *cobra.Command, args []string) {
			err := listBuilders()
			if err != nil {
				errorf("%v", err)
			}
		},
	}
	adapterCmd.AddCommand(&listCmd)

	sa := &serverArgs{}
	serverCmd := cobra.Command{
		Use:   "server",
		Short: "Starts the mixer as a server",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Starting gRPC server on port %v", sa.port)

			err := runServer(sa)
			if err != nil {
				errorf("%v", err)
			}
		},
	}
	serverCmd.PersistentFlags().UintVarP(&sa.port, "port", "p", 9091, "TCP port to use for the mixer's gRPC API")
	serverCmd.PersistentFlags().UintVarP(&sa.maxMessageSize, "maxMessageSize", "", 1024*1024, "Maximum size of individual gRPC messages")
	serverCmd.PersistentFlags().UintVarP(&sa.maxConcurrentStreams, "maxConcurrentStreams", "", 32, "Maximum supported number of concurrent gRPC streams")
	serverCmd.PersistentFlags().BoolVarP(&sa.compressedPayload, "compressedPayload", "", false, "Whether to compress gRPC messages")
	serverCmd.PersistentFlags().StringVarP(&sa.serverCertFile, "serverCertFile", "", "", "The TLS cert file")
	serverCmd.PersistentFlags().StringVarP(&sa.serverKeyFile, "serverKeyFile", "", "", "The TLS key file")
	serverCmd.PersistentFlags().StringVarP(&sa.clientCertFiles, "clientCertFiles", "", "", "A set of comma-separated client X509 cert files")
	rootCmd.AddCommand(&serverCmd)

	if err := rootCmd.Execute(); err != nil {
		errorf("%v", err)
	}
}

func main() {
	withArgs(os.Args[1:],
		func(format string, a ...interface{}) {
			glog.Errorf(format+"\n", a...)
			os.Exit(1)
		})
}
