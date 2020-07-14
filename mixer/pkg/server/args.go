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

package server

import (
	"bytes"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/loadshedding"
	"istio.io/istio/mixer/pkg/runtime/config/constant"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/istio/pkg/tracing"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/log"
	"istio.io/pkg/probe"
)

// Args contains the startup arguments to instantiate Mixer.
type Args struct {
	// The templates to register.
	Templates map[string]template.Info

	// The adapters to use
	Adapters []adapter.InfoFn

	// Maximum size of individual gRPC messages
	MaxMessageSize uint

	// Maximum number of outstanding RPCs per connection
	MaxConcurrentStreams uint

	// Maximum number of goroutines in the API worker pool
	APIWorkerPoolSize int

	// Maximum number of goroutines in the adapter worker pool
	AdapterWorkerPoolSize int

	// URL of the config store. Use k8s://path_to_kubeconfig, fs:// for file system, or mcps://<host> to
	// connect to Galley. If path_to_kubeconfig is empty, in-cluster kubeconfig is used.")
	// If this is empty (and ConfigStore isn't specified), "k8s://" will be used.
	ConfigStoreURL string

	// The certificate file locations for the MCP config backend.
	CredentialOptions *creds.Options

	// For testing; this one is used for the backend store if ConfigStoreURL is empty. Specifying both is invalid.
	ConfigStore store.Store

	// Kubernetes namespace used to store mesh-wide configuration.")
	ConfigDefaultNamespace string

	// Timeout until the initial set of configurations are received, before declaring as ready.
	ConfigWaitTimeout time.Duration

	// The logging options to use
	LoggingOptions *log.Options

	// The tracing options to use
	TracingOptions *tracing.Options

	// The path to the file which indicates the liveness of the server by its existence.
	// This will be used for k8s liveness probe. If empty, it does nothing.
	LivenessProbeOptions *probe.Options

	// The path to the file for readiness probe, similar to LivenessProbePath.
	ReadinessProbeOptions *probe.Options

	// The introspection options to use
	IntrospectionOptions *ctrlz.Options

	// Address to use for Mixer's gRPC API. This setting supersedes the API port setting.
	APIAddress string

	// Port to use for Mixer's gRPC API
	APIPort uint16

	// Port to use for exposing mixer self-monitoring information
	MonitoringPort uint16

	// Maximum number of entries in the check cache
	NumCheckCacheEntries int32

	// Enable profiling via web interface host:port/debug/pprof
	EnableProfiling bool

	// Enables gRPC-level tracing
	EnableGRPCTracing bool

	// If true, each request to Mixer will be executed in a single go routine (useful for debugging)
	SingleThreaded bool

	// Whether or not to establish watches for adapter-specific CRDs
	UseAdapterCRDs bool

	// Whether or not to establish watches for template-specific CRDs
	UseTemplateCRDs bool

	// Namespaces the controller watches
	WatchedNamespaces string

	LoadSheddingOptions loadshedding.Options
}

// DefaultArgs allocates an Args struct initialized with Mixer's default configuration.
func DefaultArgs() *Args {
	return &Args{
		APIPort:                9091,
		MonitoringPort:         15014,
		MaxMessageSize:         1024 * 1024,
		MaxConcurrentStreams:   1024,
		APIWorkerPoolSize:      1024,
		AdapterWorkerPoolSize:  1024,
		CredentialOptions:      creds.DefaultOptions(),
		ConfigDefaultNamespace: constant.DefaultConfigNamespace,
		ConfigWaitTimeout:      2 * time.Minute,
		LoggingOptions:         log.DefaultOptions(),
		TracingOptions:         tracing.DefaultOptions(),
		LivenessProbeOptions:   &probe.Options{},
		ReadinessProbeOptions:  &probe.Options{},
		IntrospectionOptions:   ctrlz.DefaultOptions(),
		EnableProfiling:        true,
		NumCheckCacheEntries:   5000 * 5 * 60, // 5000 QPS with average TTL of 5 minutes
		UseAdapterCRDs:         true,
		UseTemplateCRDs:        true,
		WatchedNamespaces:      metav1.NamespaceAll,
		LoadSheddingOptions:    loadshedding.DefaultOptions(),
	}
}

func (a *Args) validate() error {
	if a.MaxMessageSize == 0 {
		return fmt.Errorf("max message size must be > 0, got %d", a.MaxMessageSize)
	}

	if a.MaxConcurrentStreams == 0 {
		return fmt.Errorf("max concurrent streams must be > 0, got %d", a.MaxConcurrentStreams)
	}

	if a.APIWorkerPoolSize <= 0 {
		return fmt.Errorf("api worker pool size must be > 0, got pool size %d", a.APIWorkerPoolSize)
	}

	if a.AdapterWorkerPoolSize <= 0 {
		return fmt.Errorf("adapter worker pool size must be > 0 , got pool size %d", a.AdapterWorkerPoolSize)
	}

	if a.NumCheckCacheEntries < 0 {
		return fmt.Errorf("# check cache entries must be >= 0 and <= 2^31-1, got %d", a.NumCheckCacheEntries)
	}

	if a.ConfigStore != nil && a.ConfigStoreURL != "" {
		return fmt.Errorf("invalid arguments: both ConfigStore and ConfigStoreURL are specified")
	}

	return nil
}

// String produces a stringified version of the arguments for debugging.
func (a *Args) String() string {
	buf := &bytes.Buffer{}

	fmt.Fprintln(buf, "MaxMessageSize: ", a.MaxMessageSize)
	fmt.Fprintln(buf, "MaxConcurrentStreams: ", a.MaxConcurrentStreams)
	fmt.Fprintln(buf, "APIWorkerPoolSize: ", a.APIWorkerPoolSize)
	fmt.Fprintln(buf, "AdapterWorkerPoolSize: ", a.AdapterWorkerPoolSize)
	fmt.Fprintln(buf, "APIPort: ", a.APIPort)
	fmt.Fprintln(buf, "APIAddress: ", a.APIAddress)
	fmt.Fprintln(buf, "MonitoringPort: ", a.MonitoringPort)
	fmt.Fprintln(buf, "EnableProfiling: ", a.EnableProfiling)
	fmt.Fprintln(buf, "SingleThreaded: ", a.SingleThreaded)
	fmt.Fprintln(buf, "NumCheckCacheEntries: ", a.NumCheckCacheEntries)
	fmt.Fprintln(buf, "ConfigStoreURL: ", a.ConfigStoreURL)
	fmt.Fprintln(buf, "CertificateFile: ", a.CredentialOptions.CertificateFile)
	fmt.Fprintln(buf, "KeyFile: ", a.CredentialOptions.KeyFile)
	fmt.Fprintln(buf, "CACertificateFile: ", a.CredentialOptions.CACertificateFile)
	fmt.Fprintln(buf, "ConfigDefaultNamespace: ", a.ConfigDefaultNamespace)
	fmt.Fprintln(buf, "ConfigWaitTimeout: ", a.ConfigWaitTimeout)
	fmt.Fprintln(buf, "WatchedNamespaces: ", a.WatchedNamespaces)
	fmt.Fprintf(buf, "LoggingOptions: %#v\n", *a.LoggingOptions)
	fmt.Fprintf(buf, "TracingOptions: %#v\n", *a.TracingOptions)
	fmt.Fprintf(buf, "IntrospectionOptions: %#v\n", *a.IntrospectionOptions)
	fmt.Fprintf(buf, "UseTemplateCRDs: %#v\n", a.UseTemplateCRDs)
	fmt.Fprintf(buf, "LoadSheddingOptions: %#v\n", a.LoadSheddingOptions)
	fmt.Fprintf(buf, "UseAdapterCRDs: %#v\n", a.UseAdapterCRDs)

	return buf.String()
}
