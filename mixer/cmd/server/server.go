// Copyright 2016 Istio Authors
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
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/api"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/pool"
	"istio.io/mixer/pkg/tracing"
)

type serverArgs struct {
	port                 uint
	maxMessageSize       uint
	maxConcurrentStreams uint
	workerPoolSize       uint
	compressedPayload    bool
	enableTracing        bool
	serverCertFile       string
	serverKeyFile        string
	clientCertFiles      string

	// mixer manager args
	serviceConfigFile      string
	globalConfigFile       string
	configFetchIntervalSec uint
}

func serverCmd(errorf errorFn) *cobra.Command {
	sa := &serverArgs{}
	serverCmd := cobra.Command{
		Use:   "server",
		Short: "Starts the mixer as a server",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Starting gRPC server on port %v\n", sa.port)

			err := runServer(sa)
			if err != nil {
				errorf("%v", err)
			}
		},
	}
	serverCmd.PersistentFlags().UintVarP(&sa.port, "port", "p", 9091, "TCP port to use for the mixer's gRPC API")
	serverCmd.PersistentFlags().UintVarP(&sa.maxMessageSize, "maxMessageSize", "", 1024*1024, "Maximum size of individual gRPC messages")
	serverCmd.PersistentFlags().UintVarP(&sa.maxConcurrentStreams, "maxConcurrentStreams", "", 32, "Maximum supported number of concurrent gRPC streams")
	serverCmd.PersistentFlags().UintVarP(&sa.workerPoolSize, "workerPoolSize", "", 1024, "Number of goroutines in the global worker pool")
	serverCmd.PersistentFlags().BoolVarP(&sa.compressedPayload, "compressedPayload", "", false, "Whether to compress gRPC messages")
	serverCmd.PersistentFlags().StringVarP(&sa.serverCertFile, "serverCertFile", "", "", "The TLS cert file")
	serverCmd.PersistentFlags().StringVarP(&sa.serverKeyFile, "serverKeyFile", "", "", "The TLS key file")
	serverCmd.PersistentFlags().StringVarP(&sa.clientCertFiles, "clientCertFiles", "", "", "A set of comma-separated client X509 cert files")
	// TODO: implement an option to specify how traces are reported (hardcoded to report to stdout right now).
	serverCmd.PersistentFlags().BoolVarP(&sa.enableTracing, "trace", "", false, "Whether to trace rpc executions")

	// mixer manager args

	serverCmd.PersistentFlags().StringVarP(&sa.serviceConfigFile, "serviceConfigFile", "", "serviceConfig.yml", "Combined Service Config")
	serverCmd.PersistentFlags().StringVarP(&sa.globalConfigFile, "globalConfigFile", "", "globalConfig.yml", "Global Config")
	serverCmd.PersistentFlags().UintVarP(&sa.configFetchIntervalSec, "configFetchInterval", "", 5, "Config fetch interval in seconds")

	return &serverCmd
}

// The queue depth for the global worker pool
const globalPoolQueueDepth = 128

func runServer(sa *serverArgs) error {
	poolSize := int(sa.workerPoolSize)
	if poolSize <= 0 {
		return fmt.Errorf("worker pool size must be >= 0 and <= 2^31-1, got pool size %d", poolSize)
	}

	var gp *pool.GoroutinePool
	if poolSize > 1 {
		gp = pool.NewGoroutinePool(globalPoolQueueDepth)
		gp.AddWorkers(poolSize)
	}

	// get aspect registry with proper aspect --> api mappings
	eval := expr.NewCEXLEvaluator()
	adapterMgr := adapterManager.NewManager(adapter.Inventory(), aspect.Inventory(), eval, gp)
	configManager := config.NewManager(eval, adapterMgr.AspectValidatorFinder(), adapterMgr.BuilderValidatorFinder(),
		adapterMgr.AdapterToAspectMapperFunc(),
		sa.globalConfigFile, sa.serviceConfigFile, time.Second*time.Duration(sa.configFetchIntervalSec))

	handler := api.NewHandler(adapterMgr, adapterMgr.MethodMap())

	var serverCert *tls.Certificate
	var clientCerts *x509.CertPool

	if sa.serverCertFile != "" && sa.serverKeyFile != "" {
		sc, err := tls.LoadX509KeyPair(sa.serverCertFile, sa.serverKeyFile)
		if err != nil {
			return fmt.Errorf("failed to load server certificate and server key: %v", err)
		}
		serverCert = &sc
	}

	if sa.clientCertFiles != "" {
		clientCerts = x509.NewCertPool()
		for _, clientCertFile := range strings.Split(sa.clientCertFiles, ",") {
			pem, err := ioutil.ReadFile(clientCertFile)
			if err != nil {
				return fmt.Errorf("failed to load client certificate: %v", err)
			}
			clientCerts.AppendCertsFromPEM(pem)
		}
	}

	// get the network stuff setup
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", uint16(sa.port)))
	if err != nil {
		return err
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

	configManager.Register(handler.(config.ChangeListener))
	configManager.Start()

	// get everything wired up
	gs := grpc.NewServer(grpcOptions...)
	s := api.NewGRPCServer(handler, tracer)
	mixerpb.RegisterMixerServer(gs, s)

	return gs.Serve(listener)
}
