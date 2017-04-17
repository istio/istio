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
	"istio.io/mixer/pkg/version"
)

type serverArgs struct {
	maxMessageSize                uint
	maxConcurrentStreams          uint
	apiWorkerPoolSize             int
	adapterWorkerPoolSize         int
	port                          uint16
	configAPIPort                 uint16
	singleThreaded                bool
	compressedPayload             bool
	enableTracing                 bool
	serverCertFile                string
	serverKeyFile                 string
	clientCertFiles               string
	configStoreURL                string
	configFetchIntervalSec        uint
	configIdentityAttribute       string
	configIdentityAttributeDomain string

	// @deprecated
	serviceConfigFile string
	// @deprecated
	globalConfigFile string
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
	serverCmd.PersistentFlags().Uint16VarP(&sa.configAPIPort, "configAPIPort", "", 9094, "HTTP port to use for the mixer's Configuration API")
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

	serverCmd.PersistentFlags().StringVarP(&sa.configStoreURL, "configStoreURL", "", "",
		"URL of the config store. May be fs:// for file system, or redis:// for redis url")

	// Hide configIdentityAttribute and configIdentityAttributeDomain until we have a need to expose it.
	// These parameters ensure that rest of the mixer makes no assumptions about specific identity attribute.
	// Rules selection is based on scopes.
	serverCmd.PersistentFlags().StringVarP(&sa.configIdentityAttribute, "configIdentityAttribute", "", "target.service",
		"Attribute that is used to identify applicable scopes.")
	if err := serverCmd.PersistentFlags().MarkHidden("configIdentityAttribute"); err != nil {
		fatalf("unable to hide: %v", err)
	}
	serverCmd.PersistentFlags().StringVarP(&sa.configIdentityAttributeDomain, "configIdentityAttributeDomain", "", "svc.cluster.local",
		"The domain to which all values of the configIdentityAttribute belong. For kubernetes services it is svc.cluster.local")
	if err := serverCmd.PersistentFlags().MarkHidden("configIdentityAttributeDomain"); err != nil {
		fatalf("unable to hide: %v", err)
	}

	// serviceConfig and gobalConfig are for compatibility only
	serverCmd.PersistentFlags().StringVarP(&sa.serviceConfigFile, "serviceConfigFile", "", "", "Combined Service Config")
	serverCmd.PersistentFlags().StringVarP(&sa.globalConfigFile, "globalConfigFile", "", "", "Global Config")

	serverCmd.PersistentFlags().UintVarP(&sa.configFetchIntervalSec, "configFetchInterval", "", 5, "Configuration fetch interval in seconds")
	return &serverCmd
}

// configStore - given config this function returns a KeyValueStore
// It provides a compatibility layer so one can continue using serviceConfigFile and globalConfigFile flags
// until they are removed.
func configStore(url, serviceConfigFile, globalConfigFile string, printf, fatalf shared.FormatFn) (store config.KeyValueStore) {
	var err error
	if url != "" {
		if store, err = config.NewStore(url); err != nil {
			fatalf("Failed to get config store: %v", err)
		}
		return store
	}
	if serviceConfigFile == "" || globalConfigFile == "" {
		fatalf("Missing configStoreURL")
	}
	printf("*** serviceConfigFile and globalConfigFile are deprecated, use configStoreURL")
	if store, err = config.NewCompatFSStore(globalConfigFile, serviceConfigFile); err != nil {
		fatalf("Failed to get config store: %v", err)
	}
	return store
}

func runServer(sa *serverArgs, printf, fatalf shared.FormatFn) {
	var err error
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
	store := configStore(sa.configStoreURL, sa.serviceConfigFile, sa.globalConfigFile, printf, fatalf)
	configManager := config.NewManager(eval, adapterMgr.AspectValidatorFinder, adapterMgr.BuilderValidatorFinder,
		adapterMgr.SupportedKinds,
		store, time.Second*time.Duration(sa.configFetchIntervalSec),
		sa.configIdentityAttribute,
		sa.configIdentityAttributeDomain)

	configAPIServer := config.NewAPI("v1", sa.configAPIPort, eval,
		adapterMgr.AspectValidatorFinder, adapterMgr.BuilderValidatorFinder,
		adapterMgr.SupportedKinds, store)

	var serverCert *tls.Certificate
	var clientCerts *x509.CertPool

	if sa.serverCertFile != "" && sa.serverKeyFile != "" {
		var sc tls.Certificate
		if sc, err = tls.LoadX509KeyPair(sa.serverCertFile, sa.serverKeyFile); err != nil {
			fatalf("Failed to load server certificate and server key: %v", err)
		}
		serverCert = &sc
	}

	if sa.clientCertFiles != "" {
		clientCerts = x509.NewCertPool()
		for _, clientCertFile := range strings.Split(sa.clientCertFiles, ",") {
			var pem []byte
			if pem, err = ioutil.ReadFile(clientCertFile); err != nil {
				fatalf("Failed to load client certificate: %v", err)
			}
			clientCerts.AppendCertsFromPEM(pem)
		}
	}

	var listener net.Listener
	// get the network stuff setup
	if listener, err = net.Listen("tcp", fmt.Sprintf(":%d", sa.port)); err != nil {
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

	printf("Starting Config API server on port %v", sa.configAPIPort)
	go configAPIServer.Run()

	// get everything wired up
	gs := grpc.NewServer(grpcOptions...)
	s := api.NewGRPCServer(adapterMgr, tracer, gp)
	mixerpb.RegisterMixerServer(gs, s)

	printf("Istio Mixer: %s", version.Info)
	printf("Starting gRPC server on port %v", sa.port)

	if err = gs.Serve(listener); err != nil {
		fatalf("Failed serving gRPC server: %v", err)
	}
}
