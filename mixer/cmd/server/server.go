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
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	bt "github.com/opentracing/basictracer-go"
	ot "github.com/opentracing/opentracing-go"
	"github.com/spf13/cobra"

	"istio.io/mixer/adapter"
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/api"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/attribute"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/expr"
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
	// TODO: define a sensible size for our worker pool; we probably want (max outstanding requests allowed)*(number of adapters executed per request)
	serverCmd.PersistentFlags().UintVarP(&sa.workerPoolSize, "workerPoolSize", "", 100,
		"Number of workers used to execute adapters in the entire server; this should be roughly (max outstanding requests)*(number of adapters per request)."+
			"If workerPoolSize is exactly zero all adapters are executed serially on their request go routine.")
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

func serverOpts(sa *serverArgs, handlers api.Handler) (*api.GRPCServerOptions, error) {
	var serverCert *tls.Certificate
	var clientCerts *x509.CertPool

	if sa.serverCertFile != "" && sa.serverKeyFile != "" {
		sc, err := tls.LoadX509KeyPair(sa.serverCertFile, sa.serverKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load server certificate and server key: %v", err)
		}
		serverCert = &sc
	}

	if sa.clientCertFiles != "" {
		clientCerts = x509.NewCertPool()
		for _, clientCertFile := range strings.Split(sa.clientCertFiles, ",") {
			pem, err := ioutil.ReadFile(clientCertFile)
			if err != nil {
				return nil, fmt.Errorf("failed to load client certificate: %v", err)
			}
			clientCerts.AppendCertsFromPEM(pem)
		}
	}

	var tracer ot.Tracer
	if sa.enableTracing {
		tracer = bt.New(tracing.IORecorder(os.Stdout))
	}

	return &api.GRPCServerOptions{
		Port:                 uint16(sa.port),
		MaxMessageSize:       sa.maxMessageSize,
		MaxConcurrentStreams: sa.maxConcurrentStreams,
		CompressedPayload:    sa.compressedPayload,
		ServerCertificate:    serverCert,
		ClientCertificates:   clientCerts,
		Handler:              handlers,
		AttributeManager:     attribute.NewManager(),
		Tracer:               tracer,
	}, nil
}

func runServer(sa *serverArgs) error {
	// get aspect registry with proper aspect --> api mappings
	eval := expr.NewIdentityEvaluator()
	adapterMgr := adapterManager.NewManager(adapter.Inventory(), aspect.Inventory(), eval)
	configManager := config.NewManager(eval, adapterMgr.AspectValidatorFinder(), adapterMgr.BuilderValidatorFinder(),
		adapterMgr.AdapterToAspectMapperFunc(),
		sa.globalConfigFile, sa.serviceConfigFile, time.Second*time.Duration(sa.configFetchIntervalSec))

	var handler api.Handler
	if sa.workerPoolSize == 0 {
		handler = api.NewHandler(adapterMgr, adapterMgr.MethodMap())
	} else {
		poolsize := int(sa.workerPoolSize)
		if poolsize < 0 {
			return fmt.Errorf("worker pool size must be less than int max value, got pool size %d", poolsize)
		}
		handler = api.NewHandler(adapterManager.NewParallelManager(adapterMgr, poolsize), adapterMgr.MethodMap())
	}

	grpcServerOptions, err := serverOpts(sa, handler)
	if err != nil {
		return err
	}

	configManager.Register(handler.(config.ChangeListener))
	configManager.Start()

	var grpcServer *api.GRPCServer
	if grpcServer, err = api.NewGRPCServer(grpcServerOptions); err != nil {
		return fmt.Errorf("unable to initialize gRPC server " + err.Error())
	}
	return grpcServer.Start()
}
