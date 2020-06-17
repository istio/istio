// Copyright Istio Authors
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
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/pkg/version"
)

func serverCmd(info map[string]template.Info, adapters []adapter.InfoFn, printf, fatalf shared.FormatFn) *cobra.Command {
	sa := server.DefaultArgs()
	sa.Templates = info
	sa.Adapters = adapters

	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Starts Mixer as a server",
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			runServer(sa, printf, fatalf)
		},
	}

	serverCmd.PersistentFlags().Uint16VarP(&sa.APIPort, "port", "p", sa.APIPort,
		"TCP port to use for Mixer's gRPC API, if the address option is not specified")
	serverCmd.PersistentFlags().StringVarP(&sa.APIAddress, "address", "", sa.APIAddress,
		"Address to use for Mixer's gRPC API, e.g. tcp://127.0.0.1:9092 or unix:///path/to/file")
	serverCmd.PersistentFlags().Uint16Var(&sa.MonitoringPort, "monitoringPort", sa.MonitoringPort,
		"HTTP port to use for Mixer self-monitoring information")
	serverCmd.PersistentFlags().UintVarP(&sa.MaxMessageSize, "maxMessageSize", "", sa.MaxMessageSize,
		"Maximum size of individual gRPC messages")
	serverCmd.PersistentFlags().UintVarP(&sa.MaxConcurrentStreams, "maxConcurrentStreams", "", sa.MaxConcurrentStreams,
		"Maximum number of outstanding RPCs per connection")
	serverCmd.PersistentFlags().IntVarP(&sa.APIWorkerPoolSize, "apiWorkerPoolSize", "", sa.APIWorkerPoolSize,
		"Max number of goroutines in the API worker pool")
	serverCmd.PersistentFlags().IntVarP(&sa.AdapterWorkerPoolSize, "adapterWorkerPoolSize", "", sa.AdapterWorkerPoolSize,
		"Max number of goroutines in the adapter worker pool")
	serverCmd.PersistentFlags().BoolVarP(&sa.SingleThreaded, "singleThreaded", "", sa.SingleThreaded,
		"If true, each request to Mixer will be executed in a single go routine (useful for debugging)")
	serverCmd.PersistentFlags().Int32VarP(&sa.NumCheckCacheEntries, "numCheckCacheEntries", "", sa.NumCheckCacheEntries,
		"Max number of entries in the check result cache")

	serverCmd.PersistentFlags().StringVarP(&sa.ConfigStoreURL, "configStoreURL", "", sa.ConfigStoreURL,
		"URL of the config store. Use k8s://path_to_kubeconfig, fs:// for file system, or mcps://<address> for MCP/Galley. "+
			"If path_to_kubeconfig is empty, in-cluster kubeconfig is used.")

	serverCmd.PersistentFlags().StringVarP(&sa.ConfigDefaultNamespace, "configDefaultNamespace", "", sa.ConfigDefaultNamespace,
		"Namespace used to store mesh wide configuration.")
	serverCmd.PersistentFlags().DurationVarP(&sa.ConfigWaitTimeout, "configWaitTimeout", "", sa.ConfigWaitTimeout,
		"Timeout until the initial set of configurations are received, before declaring as ready.")

	serverCmd.PersistentFlags().StringVar(&sa.LivenessProbeOptions.Path, "livenessProbePath", sa.LivenessProbeOptions.Path,
		"Path to the file for the liveness probe.")
	serverCmd.PersistentFlags().DurationVar(&sa.LivenessProbeOptions.UpdateInterval, "livenessProbeInterval", sa.LivenessProbeOptions.UpdateInterval,
		"Interval of updating file for the liveness probe.")
	serverCmd.PersistentFlags().StringVar(&sa.ReadinessProbeOptions.Path, "readinessProbePath", sa.ReadinessProbeOptions.Path,
		"Path to the file for the readiness probe.")
	serverCmd.PersistentFlags().DurationVar(&sa.ReadinessProbeOptions.UpdateInterval, "readinessProbeInterval", sa.ReadinessProbeOptions.UpdateInterval,
		"Interval of updating file for the readiness probe.")
	serverCmd.PersistentFlags().BoolVar(&sa.EnableProfiling, "profile", sa.EnableProfiling,
		"Enable profiling via web interface host:port/debug/pprof")

	serverCmd.PersistentFlags().BoolVar(&sa.UseAdapterCRDs, "useAdapterCRDs", sa.UseAdapterCRDs,
		"Whether or not to allow configuration of Mixer via adapter-specific CRDs")
	serverCmd.PersistentFlags().BoolVar(&sa.UseTemplateCRDs, "useTemplateCRDs", sa.UseTemplateCRDs,
		"Whether or not to allow configuration of Mixer via template-specific CRDs")

	serverCmd.PersistentFlags().StringVar(&sa.WatchedNamespaces, "namespaces", sa.WatchedNamespaces,
		"List of namespaces to watch, separated by comma; if not set, watch all namespaces")

	sa.CredentialOptions.AttachCobraFlags(serverCmd)
	sa.LoggingOptions.AttachCobraFlags(serverCmd)
	sa.TracingOptions.AttachCobraFlags(serverCmd)
	sa.IntrospectionOptions.AttachCobraFlags(serverCmd)
	sa.LoadSheddingOptions.AttachCobraFlags(serverCmd)

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
