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

package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"time"

	bt "github.com/opentracing/basictracer-go"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/adapter"
	"istio.io/mixer/cmd/shared"
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/api"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/tracing"
)

type serverArgs struct {
	maxMessageSize         uint
	maxConcurrentStreams   uint
	apiWorkerPoolSize      int
	adapterWorkerPoolSize  int
	port                   uint16
	singleThreaded         bool
	compressedPayload      bool
	enableTracing          bool
	serverCertFile         string
	serverKeyFile          string
	clientCertFiles        string
	serviceConfigFile      string
	globalConfigFile       string
	configFetchIntervalSec uint
}

func serverCmd(printf, fatalf shared.FormatFn) *cobra.Command {
	sa := &serverArgs{}
	serverCmd := cobra.Command{
		Use:   "server",
		Short: "Starts the mixer as a server",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if sa.apiWorkerPoolSize <= 0 {
				return fmt.Errorf("api worker pool size must be >= 0 and <= 2^31-1, got pool size %d", sa.apiWorkerPoolSize)
			}

			if sa.adapterWorkerPoolSize <= 0 {
				return fmt.Errorf("adapter worker pool size must be >= 0 and <= 2^31-1, got pool size %d", sa.adapterWorkerPoolSize)
			}

			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			runServer(sa, printf, fatalf)
		},
	}
	serverCmd.PersistentFlags().Uint16VarP(&sa.port, "port", "p", 9091, "TCP port to use for the mixer's gRPC API")
	serverCmd.PersistentFlags().UintVarP(&sa.maxMessageSize, "maxMessageSize", "", 1024*1024, "Maximum size of individual gRPC messages")
	serverCmd.PersistentFlags().UintVarP(&sa.maxConcurrentStreams, "maxConcurrentStreams", "", 32, "Maximum supported number of concurrent gRPC streams")
	serverCmd.PersistentFlags().IntVarP(&sa.apiWorkerPoolSize, "apiWorkerPoolSize", "", 1024, "Max # of goroutines in the API worker pool")
	serverCmd.PersistentFlags().IntVarP(&sa.adapterWorkerPoolSize, "adapterWorkerPoolSize", "", 1024, "Max # of goroutines in the adapter worker pool")
	serverCmd.PersistentFlags().BoolVarP(&sa.singleThreaded, "singleThreaded", "", false, "Whether to run the mixer in single-threaded mode (useful "+
		"for debugging)")
	serverCmd.PersistentFlags().BoolVarP(&sa.compressedPayload, "compressedPayload", "", false, "Whether to compress gRPC messages")

	serverCmd.PersistentFlags().StringVarP(&sa.serverCertFile, "serverCertFile", "", "", "The TLS cert file")
	_ = serverCmd.MarkPersistentFlagFilename("serverCertFile")

	serverCmd.PersistentFlags().StringVarP(&sa.serverKeyFile, "serverKeyFile", "", "", "The TLS key file")
	_ = serverCmd.MarkPersistentFlagFilename("serverKeyFile")

	serverCmd.PersistentFlags().StringVarP(&sa.clientCertFiles, "clientCertFiles", "", "", "A set of comma-separated client X509 cert files")

	// TODO: implement an option to specify how traces are reported (hardcoded to report to stdout right now).
	serverCmd.PersistentFlags().BoolVarP(&sa.enableTracing, "trace", "", false, "Whether to trace rpc executions")

	serverCmd.PersistentFlags().StringVarP(&sa.serviceConfigFile, "serviceConfigFile", "", "serviceConfig.yml", "Combined Service Config")
	_ = serverCmd.MarkPersistentFlagFilename("serverConfigFile", "yaml", "yml")

	serverCmd.PersistentFlags().StringVarP(&sa.globalConfigFile, "globalConfigFile", "", "globalConfig.yml", "Global Config")
	_ = serverCmd.MarkPersistentFlagFilename("globalConfigFile", "yaml", "yml")

	serverCmd.PersistentFlags().UintVarP(&sa.configFetchIntervalSec, "configFetchInterval", "", 5, "Configuration fetch interval in seconds")

	return &serverCmd
}

func runServer(sa *serverArgs, printf, fatalf shared.FormatFn) {
	apiPoolSize := sa.apiWorkerPoolSize
	adapterPoolSize := sa.adapterWorkerPoolSize

	gp := pool.NewGoroutinePool(apiPoolSize, sa.singleThreaded)
	gp.AddWorkers(apiPoolSize)
	defer gp.Close()

	adapterGP := pool.NewGoroutinePool(adapterPoolSize, sa.singleThreaded)
	adapterGP.AddWorkers(adapterPoolSize)
	defer adapterGP.Close()

	// get aspect registry with proper aspect --> api mappings
	eval := expr.NewCEXLEvaluator()
	adapterMgr := adapterManager.NewManager(adapter.Inventory(), aspect.Inventory(), eval, gp, adapterGP)
	configManager := config.NewManager(eval, adapterMgr.AspectValidatorFinder, adapterMgr.BuilderValidatorFinder,
		adapterMgr.AdapterToAspectMapper,
		sa.globalConfigFile, sa.serviceConfigFile, time.Second*time.Duration(sa.configFetchIntervalSec))

	handler := api.NewHandler(adapterMgr)

	var serverCert *tls.Certificate
	var clientCerts *x509.CertPool

	if sa.serverCertFile != "" && sa.serverKeyFile != "" {
		sc, err := tls.LoadX509KeyPair(sa.serverCertFile, sa.serverKeyFile)
		if err != nil {
			fatalf("Failed to load server certificate and server key: %v", err)
		}
		serverCert = &sc
	}

	if sa.clientCertFiles != "" {
		clientCerts = x509.NewCertPool()
		for _, clientCertFile := range strings.Split(sa.clientCertFiles, ",") {
			pem, err := ioutil.ReadFile(clientCertFile)
			if err != nil {
				fatalf("Failed to load client certificate: %v", err)
			}
			clientCerts.AppendCertsFromPEM(pem)
		}
	}

	// get the network stuff setup
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", sa.port))
	if err != nil {
		fatalf("Unable to listen on socket: %v", err)
	}

	// construct the gRPC options

	var grpcOptions []grpc.ServerOption
	grpcOptions = append(grpcOptions, grpc.MaxConcurrentStreams(uint32(sa.maxConcurrentStreams)))
	grpcOptions = append(grpcOptions, grpc.MaxMsgSize(int(sa.maxMessageSize)))

	if sa.compressedPayload {
		grpcOptions = append(grpcOptions, grpc.RPCCompressor(grpc.NewGZIPCompressor()))
		grpcOptions = append(grpcOptions, grpc.RPCDecompressor(grpc.NewGZIPDecompressor()))
	}

	if serverCert != nil {
		// enable TLS
		tlsConfig := &tls.Config{}
		tlsConfig.Certificates = []tls.Certificate{*serverCert}

		if clientCerts != nil {
			// enable TLS mutual auth
			tlsConfig.ClientCAs = clientCerts
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		}
		tlsConfig.BuildNameToCertificate()

		grpcOptions = append(grpcOptions, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	var tracer tracing.Tracer
	if sa.enableTracing {
		tracer = tracing.NewTracer(bt.New(tracing.IORecorder(os.Stdout)))
	} else {
		tracer = tracing.DisabledTracer()
	}

	configManager.Register(adapterMgr)
	configManager.Start()

	// get everything wired up
	gs := grpc.NewServer(grpcOptions...)
	s := api.NewGRPCServer(handler, tracer, gp)
	mixerpb.RegisterMixerServer(gs, s)

	printf("Starting gRPC server on port %v", sa.port)

	if err = gs.Serve(listener); err != nil {
		fatalf("Failed serving gRPC server: %v", err)
	}
}
