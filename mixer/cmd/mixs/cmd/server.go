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
	"github.com/spf13/cobra"

	"istio.io/istio/mixer/cmd/shared"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/il/evaluator"
	mixerRuntime "istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/mixer/pkg/version"
)

func serverCmd(info map[string]template.Info, adapters []adapter.InfoFn, printf, fatalf shared.FormatFn) *cobra.Command {
	sa := server.NewArgs()
	sa.Templates = info
	sa.Adapters = adapters

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Starts Mixer as a server",
		Run: func(cmd *cobra.Command, args []string) {
			runServer(sa, printf, fatalf)
		},
	}

	// TODO: need to pick appropriate defaults for all these settings below

	serverCmd.PersistentFlags().Uint16VarP(&sa.APIPort, "port", "p", 9091, "TCP port to use for Mixer's gRPC API")
	serverCmd.PersistentFlags().Uint16Var(&sa.MonitoringPort, "monitoringPort", 9093, "HTTP port to use for the exposing mixer self-monitoring information")
	serverCmd.PersistentFlags().Uint16VarP(&sa.ConfigAPIPort, "configAPIPort", "", 9094, "HTTP port to use for Mixer's Configuration API")
	serverCmd.PersistentFlags().UintVarP(&sa.MaxMessageSize, "maxMessageSize", "", 1024*1024, "Maximum size of individual gRPC messages")
	serverCmd.PersistentFlags().UintVarP(&sa.MaxConcurrentStreams, "maxConcurrentStreams", "", 1024, "Maximum number of outstanding RPCs per connection")
	serverCmd.PersistentFlags().IntVarP(&sa.APIWorkerPoolSize, "apiWorkerPoolSize", "", 1024, "Max number of goroutines in the API worker pool")
	serverCmd.PersistentFlags().IntVarP(&sa.AdapterWorkerPoolSize, "adapterWorkerPoolSize", "", 1024, "Max number of goroutines in the adapter worker pool")
	// TODO: what is the right default value for expressionEvalCacheSize.
	serverCmd.PersistentFlags().IntVarP(&sa.ExpressionEvalCacheSize, "expressionEvalCacheSize", "", evaluator.DefaultCacheSize,
		"Number of entries in the expression cache")
	serverCmd.PersistentFlags().BoolVarP(&sa.SingleThreaded, "singleThreaded", "", false,
		"If true, each request to Mixer will be executed in a single go routine (useful for debugging)")

	// DEPRECATED FLAG (traceOutput). TO BE REMOVED IN SUBSEQUENT RELEASES.
	serverCmd.PersistentFlags().StringVarP(&sa.ZipkinURL, "traceOutput", "", "",
		"DEPRECATED. URL of zipkin collector (example: 'http://zipkin:9411/api/v1/spans'")
	serverCmd.PersistentFlags().MarkDeprecated("traceOutput", "please use one (or more) of the following flags: --zipkinURL, --jaegerURL, or --logTraceSpans")

	serverCmd.PersistentFlags().StringVarP(&sa.ZipkinURL, "zipkinURL", "", "",
		"URL of zipkin collector (example: 'http://zipkin:9411/api/v1/spans'). This enables tracing for Mixer itself.")
	serverCmd.PersistentFlags().StringVarP(&sa.JaegerURL, "jaegerURL", "", "",
		"URL of jaeger HTTP collector (example: 'http://jaeger:14268/api/traces?format=jaeger.thrift'). This enables tracing for Mixer itself.")
	serverCmd.PersistentFlags().BoolVarP(&sa.LogTraceSpans, "logTraceSpans", "", false,
		"Whether or not to log Mixer trace spans. This enables tracing for Mixer itself.")

	serverCmd.PersistentFlags().StringVarP(&sa.ConfigStoreURL, "configStoreURL", "", "",
		"URL of the config store. May be fs:// for file system, or redis:// for redis url")

	serverCmd.PersistentFlags().StringVarP(&sa.ConfigStore2URL, "configStore2URL", "", "",
		"URL of the config store. Use k8s://path_to_kubeconfig or fs:// for file system. If path_to_kubeconfig is empty, in-cluster kubeconfig is used.")

	serverCmd.PersistentFlags().StringVarP(&sa.ConfigDefaultNamespace, "configDefaultNamespace", "", mixerRuntime.DefaultConfigNamespace,
		"Namespace used to store mesh wide configuration.")

	// Hide configIdentityAttribute and configIdentityAttributeDomain until we have a need to expose it.
	// These parameters ensure that rest of Mixer makes no assumptions about specific identity attribute.
	// Rules selection is based on scopes.
	serverCmd.PersistentFlags().StringVarP(&sa.ConfigIdentityAttribute, "configIdentityAttribute", "", "destination.service",
		"Attribute that is used to identify applicable scopes.")
	if err := serverCmd.PersistentFlags().MarkHidden("configIdentityAttribute"); err != nil {
		fatalf("unable to hide: %v", err)
	}
	serverCmd.PersistentFlags().StringVarP(&sa.ConfigIdentityAttributeDomain, "configIdentityAttributeDomain", "", "svc.cluster.local",
		"The domain to which all values of the configIdentityAttribute belong. For kubernetes services it is svc.cluster.local")
	if err := serverCmd.PersistentFlags().MarkHidden("configIdentityAttributeDomain"); err != nil {
		fatalf("unable to hide: %v", err)
	}

	// serviceConfig and globalConfig are for compatibility only
	serverCmd.PersistentFlags().StringVarP(&sa.ServiceConfigFile, "serviceConfigFile", "", "", "Combined Service Config")
	serverCmd.PersistentFlags().StringVarP(&sa.GlobalConfigFile, "globalConfigFile", "", "", "Global Config")

	serverCmd.PersistentFlags().UintVarP(&sa.ConfigFetchIntervalSec, "configFetchInterval", "", 5, "Configuration fetch interval in seconds")

	sa.LoggingOptions.AttachCobraFlags(serverCmd)

	return serverCmd
}

func runServer(sa *server.Args, printf, fatalf shared.FormatFn) {
	printf("Mixer started with\n%s", sa)

	s, err := server.New(sa)
	if err != nil {
		fatalf("Unable to initialize Mixer: %v", err)
	}

	printf("Istio Mixer: %s", version.Info)
	printf("Starting gRPC server on port %v", sa.APIPort)

	s.Run()
	err = s.Wait()
	if err != nil {
		fatalf("Mixer unexpectedly terminated: %v", err)
	}

	_ = s.Close()
}
