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
	_ "expvar" // For /debug/vars registration. Note: temporary, NOT for general use
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	_ "net/http/pprof" // For profiling / performance investigations
	"os"
	"strings"
	"time"

	grpc_middleware "github.com/grpc-ecosystem/go-grpc-middleware"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	ot "github.com/opentracing/opentracing-go"
	zt "github.com/openzipkin/zipkin-go-opentracing"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/adapter"
	"istio.io/mixer/cmd/shared"
	pkgAdapter "istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/adapterManager"
	"istio.io/mixer/pkg/api"
	"istio.io/mixer/pkg/aspect"
	"istio.io/mixer/pkg/config"
	"istio.io/mixer/pkg/config/store"
	"istio.io/mixer/pkg/expr"
	"istio.io/mixer/pkg/il/evaluator"
	"istio.io/mixer/pkg/pool"
	mixerRuntime "istio.io/mixer/pkg/runtime"
	"istio.io/mixer/pkg/template"
	"istio.io/mixer/pkg/tracing/zipkin"
	"istio.io/mixer/pkg/version"
)

const (
	metricsPath = "/metrics"
	versionPath = "/version"
)

type serverArgs struct {
	maxMessageSize                uint
	maxConcurrentStreams          uint
	apiWorkerPoolSize             int
	adapterWorkerPoolSize         int
	expressionEvalCacheSize       int
	port                          uint16
	configAPIPort                 uint16
	monitoringPort                uint16
	singleThreaded                bool
	compressedPayload             bool
	traceOutput                   string
	serverCertFile                string
	serverKeyFile                 string
	clientCertFiles               string
	configStoreURL                string
	configStore2URL               string
	configDefaultNamespace        string
	configFetchIntervalSec        uint
	configIdentityAttribute       string
	configIdentityAttributeDomain string
	useAst                        bool

	// @deprecated
	serviceConfigFile string
	// @deprecated
	globalConfigFile string
}

func serverCmd(info map[string]template.Info, adapters []pkgAdapter.InfoFn, printf, fatalf shared.FormatFn) *cobra.Command {
	sa := &serverArgs{}
	serverCmd := cobra.Command{
		Use:   "server",
		Short: "Starts Mixer as a server",
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
			runServer(sa, info, adapters, printf, fatalf)
		},
	}

	// TODO: need to pick appropriate defaults for all these settings below

	serverCmd.PersistentFlags().Uint16VarP(&sa.port, "port", "p", 9091, "TCP port to use for Mixer's gRPC API")
	serverCmd.PersistentFlags().Uint16Var(&sa.monitoringPort, "monitoringPort", 9093, "HTTP port to use for the exposing mixer self-monitoring information")
	serverCmd.PersistentFlags().Uint16VarP(&sa.configAPIPort, "configAPIPort", "", 9094, "HTTP port to use for Mixer's Configuration API")
	serverCmd.PersistentFlags().UintVarP(&sa.maxMessageSize, "maxMessageSize", "", 1024*1024, "Maximum size of individual gRPC messages")
	serverCmd.PersistentFlags().UintVarP(&sa.maxConcurrentStreams, "maxConcurrentStreams", "", 1024, "Maximum number of outstanding RPCs per connection")
	serverCmd.PersistentFlags().IntVarP(&sa.apiWorkerPoolSize, "apiWorkerPoolSize", "", 1024, "Max number of goroutines in the API worker pool")
	serverCmd.PersistentFlags().IntVarP(&sa.adapterWorkerPoolSize, "adapterWorkerPoolSize", "", 1024, "Max number of goroutines in the adapter worker pool")
	// TODO: what is the right default value for expressionEvalCacheSize.
	serverCmd.PersistentFlags().IntVarP(&sa.expressionEvalCacheSize, "expressionEvalCacheSize", "", expr.DefaultCacheSize,
		"Number of entries in the expression cache")
	serverCmd.PersistentFlags().BoolVarP(&sa.singleThreaded, "singleThreaded", "", false,
		"If true, each request to Mixer will be executed in a single go routine (useful for debugging)")
	serverCmd.PersistentFlags().BoolVarP(&sa.compressedPayload, "compressedPayload", "", false, "Whether to compress gRPC messages")

	serverCmd.PersistentFlags().StringVarP(&sa.serverCertFile, "serverCertFile", "", "", "The TLS cert file")
	_ = serverCmd.MarkPersistentFlagFilename("serverCertFile")

	serverCmd.PersistentFlags().StringVarP(&sa.serverKeyFile, "serverKeyFile", "", "", "The TLS key file")
	_ = serverCmd.MarkPersistentFlagFilename("serverKeyFile")

	serverCmd.PersistentFlags().StringVarP(&sa.clientCertFiles, "clientCertFiles", "", "", "A set of comma-separated client X509 cert files")

	// TODO: implement a better option to specify how traces are reported
	serverCmd.PersistentFlags().StringVarP(&sa.traceOutput, "traceOutput", "t", "",
		"If the literal string 'STDOUT' or 'STDERR', traces will be produced and written to stdout or stderr respectively. "+
			"Otherwise the address is assumed to be a URL and HTTP zipkin traces are sent to that address. "+
			"Note that when providing a URL it must be the full path to the span collection endpoint, e.g. 'http://zipkin:9411/api/v1/spans'.")

	serverCmd.PersistentFlags().StringVarP(&sa.configStoreURL, "configStoreURL", "", "",
		"URL of the config store. May be fs:// for file system, or redis:// for redis url")

	serverCmd.PersistentFlags().StringVarP(&sa.configStore2URL, "configStore2URL", "", "",
		"URL of the config store. Use k8s://path_to_kubeconfig or fs:// for file system. If path_to_kubeconfig is empty, in-cluster kubeconfig is used.")

	serverCmd.PersistentFlags().StringVarP(&sa.configDefaultNamespace, "configDefaultNamespace", "", "istio-config-default",
		"Namespace used to store mesh wide configuration.")

	// Hide configIdentityAttribute and configIdentityAttributeDomain until we have a need to expose it.
	// These parameters ensure that rest of Mixer makes no assumptions about specific identity attribute.
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

	serverCmd.PersistentFlags().BoolVarP(&sa.useAst, "useAst", "", false,
		"Use AST instead of Mixer IL to evaluate configuration against the adapters.")

	// serviceConfig and gobalConfig are for compatibility only
	serverCmd.PersistentFlags().StringVarP(&sa.serviceConfigFile, "serviceConfigFile", "", "", "Combined Service Config")
	serverCmd.PersistentFlags().StringVarP(&sa.globalConfigFile, "globalConfigFile", "", "", "Global Config")

	serverCmd.PersistentFlags().UintVarP(&sa.configFetchIntervalSec, "configFetchInterval", "", 5, "Configuration fetch interval in seconds")
	return &serverCmd
}

// configStore - given config this function returns a KeyValueStore
// It provides a compatibility layer so one can continue using serviceConfigFile and globalConfigFile flags
// until they are removed.
func configStore(url, serviceConfigFile, globalConfigFile string, printf, fatalf shared.FormatFn) (s store.KeyValueStore) {
	var err error
	if url != "" {
		registry := store.NewRegistry(config.StoreInventory()...)
		if s, err = registry.NewStore(url); err != nil {
			fatalf("Failed to get config store: %v", err)
		}
		return s
	}
	if serviceConfigFile == "" || globalConfigFile == "" {
		fatalf("Missing configStoreURL")
	}
	printf("*** serviceConfigFile and globalConfigFile are deprecated, use configStoreURL")
	if s, err = config.NewCompatFSStore(globalConfigFile, serviceConfigFile); err != nil {
		fatalf("Failed to get config store: %v", err)
	}
	return s
}

func runServer(sa *serverArgs, info map[string]template.Info, adapters []pkgAdapter.InfoFn, printf, fatalf shared.FormatFn) {
	printf("Mixer started with args: %#v", sa)

	var err error
	apiPoolSize := sa.apiWorkerPoolSize
	adapterPoolSize := sa.adapterWorkerPoolSize
	expressionEvalCacheSize := sa.expressionEvalCacheSize

	gp := pool.NewGoroutinePool(apiPoolSize, sa.singleThreaded)
	gp.AddWorkers(apiPoolSize)
	defer gp.Close()

	adapterGP := pool.NewGoroutinePool(adapterPoolSize, sa.singleThreaded)
	adapterGP.AddWorkers(adapterPoolSize)
	defer adapterGP.Close()

	var ilEval *evaluator.IL
	var eval expr.Evaluator
	if sa.useAst {
		// get aspect registry with proper aspect --> api mappings
		eval, err = expr.NewCEXLEvaluator(expressionEvalCacheSize)
		if err != nil {
			fatalf("Failed to create CEXL expression evaluator with cache size %d: %v", expressionEvalCacheSize, err)
		}
	} else {
		ilEval, err = evaluator.NewILEvaluator(expressionEvalCacheSize)
		if err != nil {
			fatalf("Failed to create IL expression evaluator with cache size %d: %v", expressionEvalCacheSize, err)
		}
		eval = ilEval
	}

	var dispatcher mixerRuntime.Dispatcher

	// TODO until the dispatcher 2 switch is complete,
	// dispatcher 2 is only enabled when configStore2URL is specified.
	if sa.configStore2URL != "" {
		adapterMap := adapter.InventoryMap(adapters)
		store2, err := store.NewRegistry2(config.Store2Inventory()...).NewStore2(sa.configStore2URL)
		if err != nil {
			fatalf("Failed to connect to the configuration2 server. %v", err)
		}
		dispatcher, err = mixerRuntime.New(eval, gp, adapterGP,
			sa.configIdentityAttribute, sa.configDefaultNamespace,
			store2, adapterMap, info,
		)
		if err != nil {
			fatalf("Failed to create runtime dispatcher. %v", err)
		}
	}

	repo := template.NewRepository(info)
	store := configStore(sa.configStoreURL, sa.serviceConfigFile, sa.globalConfigFile, printf, fatalf)
	adapterMgr := adapterManager.NewManager(adapter.Inventory(), aspect.Inventory(), eval, gp, adapterGP)
	configManager := config.NewManager(eval, adapterMgr.AspectValidatorFinder, adapterMgr.BuilderValidatorFinder, adapters,
		adapterMgr.SupportedKinds,
		repo, store, time.Second*time.Duration(sa.configFetchIntervalSec),
		sa.configIdentityAttribute,
		sa.configIdentityAttributeDomain)

	configAPIServer := config.NewAPI("v1", sa.configAPIPort, eval,
		adapterMgr.AspectValidatorFinder, adapterMgr.BuilderValidatorFinder, adapter.Inventory2(),
		adapterMgr.SupportedKinds, store, repo)

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

	var interceptors []grpc.UnaryServerInterceptor

	if sa.traceOutput != "" {
		var recorder zt.SpanRecorder
		switch strings.ToUpper(sa.traceOutput) {
		case "STDOUT":
			recorder = zipkin.IORecorder(os.Stdout)
			printf("Zipkin traces being dumped to stdout")
		case "STDERR":
			recorder = zipkin.IORecorder(os.Stderr)
			printf("Zipkin traces being dumped to stderr")
		default:
			col, err := zt.NewHTTPCollector(sa.traceOutput, zt.HTTPLogger(zt.LoggerFunc(func(vals ...interface{}) error {
				out := ""
				for _, val := range vals {
					out += fmt.Sprintf("%v ", val)
				}
				printf("Zipkin: %s\n", out)
				return nil
			})))
			if err != nil {
				fatalf("Unable to create zipkin http collector with address '%s': %v", sa.traceOutput, err)
			}
			recorder = zt.NewRecorder(col, false /* debug */, fmt.Sprintf("0.0.0.0:%d", sa.port), "istio-mixer")
		}
		tracer, err := zt.NewTracer(recorder, zt.ClientServerSameSpan(false))
		if err != nil {
			fatalf("Failed to construct zipkin tracer: %v", err)
		}
		printf("Zipkin traces being sent to %s", sa.traceOutput)
		ot.InitGlobalTracer(tracer)
		interceptors = append(interceptors, otgrpc.OpenTracingServerInterceptor(tracer))
	}

	// setup server prometheus monitoring (as final interceptor in chain)
	interceptors = append(interceptors, grpc_prometheus.UnaryServerInterceptor)
	grpc_prometheus.EnableHandlingTimeHistogram()
	grpcOptions = append(grpcOptions, grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(interceptors...)))

	configManager.Register(adapterMgr)
	if !sa.useAst {
		configManager.Register(ilEval)
	}
	configManager.Start()

	printf("Starting Config API server on port %v", sa.configAPIPort)
	go configAPIServer.Run()

	var monitoringListener net.Listener
	// get the network stuff setup
	if monitoringListener, err = net.Listen("tcp", fmt.Sprintf(":%d", sa.monitoringPort)); err != nil {
		fatalf("Unable to listen on socket: %v", err)
	}

	// NOTE: this is a temporary solution for provide bare-bones debug functionality
	// for mixer. a full design / implementation of self-monitoring and reporting
	// is coming. that design will include proper coverage of statusz/healthz type
	// functionality, in addition to how mixer reports its own metrics.
	http.Handle(metricsPath, promhttp.Handler())
	http.HandleFunc(versionPath, func(out http.ResponseWriter, req *http.Request) {
		if _, verErr := out.Write([]byte(version.Info.String())); verErr != nil {
			printf("error printing version info: %v", verErr)
		}
	})
	monitoring := &http.Server{Addr: fmt.Sprintf(":%d", sa.monitoringPort)}
	printf("Starting self-monitoring on port %d", sa.monitoringPort)
	go func() {
		if monErr := monitoring.Serve(monitoringListener.(*net.TCPListener)); monErr != nil {
			printf("monitoring server error: %v", monErr)
		}
	}()

	// get everything wired up
	gs := grpc.NewServer(grpcOptions...)

	// FIXME construct a runtime.New as dispatcher param
	s := api.NewGRPCServer(adapterMgr, dispatcher, gp)
	mixerpb.RegisterMixerServer(gs, s)

	printf("Istio Mixer: %s", version.Info)
	printf("Starting gRPC server on port %v", sa.port)

	if err = gs.Serve(listener); err != nil {
		fatalf("Failed serving gRPC server: %v", err)
	}
}
