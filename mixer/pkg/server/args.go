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

package server

import (
	"bytes"
	"fmt"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/il/evaluator"
	mixerRuntime "istio.io/istio/mixer/pkg/runtime"
	"istio.io/istio/mixer/pkg/template"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
	"istio.io/istio/pkg/tracing"
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

	// Maximum number of entries in the expression cache
	ExpressionEvalCacheSize int

	// URL of the config store. Use k8s://path_to_kubeconfig or fs:// for file system. If path_to_kubeconfig is empty, in-cluster kubeconfig is used.")
	// If this is empty (and ConfigStore isn't specified), "k8s://" will be used.
	ConfigStoreURL string

	// For testing; this one is used for the backend store if ConfigStoreURL is empty. Specifying both is invalid.
	ConfigStore store.Store

	// Kubernetes namespace used to store mesh-wide configuration.")
	ConfigDefaultNamespace string

	// Configuration fetch interval in seconds
	ConfigFetchIntervalSec uint

	// Attribute that is used to identify applicable scopes.
	ConfigIdentityAttribute string

	// The domain to which all values of the ConfigIdentityAttribute belong.
	// For kubernetes services it is svc.cluster.local
	ConfigIdentityAttributeDomain string

	// The logging options to use
	LoggingOptions *log.Options

	// The tracing options to use
	TracingOptions *tracing.Options

	// The path to the file which indicates the liveness of the server by its existence.
	// This will be used for k8s liveness probe. If empty, it does nothing.
	LivenessProbeOptions *probe.Options

	// The path to the file for readiness probe, similar to LivenessProbePath.
	ReadinessProbeOptions *probe.Options

	// Port to use for Mixer's gRPC API
	APIPort uint16

	// Port to use for exposing mixer self-monitoring information
	MonitoringPort uint16

	// Enables gRPC-level tracing
	EnableGRPCTracing bool

	// If true, each request to Mixer will be executed in a single go routine (useful for debugging)
	SingleThreaded bool
}

// NewArgs allocates an Args struct initialized with Mixer's default configuration.
func NewArgs() *Args {
	return &Args{
		APIPort:                       9091,
		MonitoringPort:                9093,
		MaxMessageSize:                1024 * 1024,
		MaxConcurrentStreams:          1024,
		APIWorkerPoolSize:             1024,
		AdapterWorkerPoolSize:         1024,
		ExpressionEvalCacheSize:       evaluator.DefaultCacheSize,
		ConfigDefaultNamespace:        mixerRuntime.DefaultConfigNamespace,
		ConfigIdentityAttribute:       "destination.service",
		ConfigIdentityAttributeDomain: "svc.cluster.local",
		LoggingOptions:                log.NewOptions(),
		TracingOptions:                tracing.NewOptions(),
		LivenessProbeOptions:          &probe.Options{},
		ReadinessProbeOptions:         &probe.Options{},
	}
}

func (a *Args) validate() error {
	if a.APIWorkerPoolSize <= 0 {
		return fmt.Errorf("api worker pool size must be >= 0 and <= 2^31-1, got pool size %d", a.APIWorkerPoolSize)
	}

	if a.AdapterWorkerPoolSize <= 0 {
		return fmt.Errorf("adapter worker pool size must be >= 0 and <= 2^31-1, got pool size %d", a.AdapterWorkerPoolSize)
	}

	if a.ExpressionEvalCacheSize <= 0 {
		return fmt.Errorf("expressiion evaluation cache size must be >= 0 and <= 2^31-1, got cache size %d", a.ExpressionEvalCacheSize)
	}

	return nil
}

// String produces a stringified version of the arguments for debugging.
func (a *Args) String() string {
	var b bytes.Buffer

	b.WriteString(fmt.Sprint("MaxMessageSize: ", a.MaxMessageSize, "\n"))
	b.WriteString(fmt.Sprint("MaxConcurrentStreams: ", a.MaxConcurrentStreams, "\n"))
	b.WriteString(fmt.Sprint("APIWorkerPoolSize: ", a.APIWorkerPoolSize, "\n"))
	b.WriteString(fmt.Sprint("AdapterWorkerPoolSize: ", a.AdapterWorkerPoolSize, "\n"))
	b.WriteString(fmt.Sprint("ExpressionEvalCacheSize: ", a.ExpressionEvalCacheSize, "\n"))
	b.WriteString(fmt.Sprint("APIPort: ", a.APIPort, "\n"))
	b.WriteString(fmt.Sprint("MonitoringPort: ", a.MonitoringPort, "\n"))
	b.WriteString(fmt.Sprint("SingleThreaded: ", a.SingleThreaded, "\n"))
	b.WriteString(fmt.Sprint("ConfigStoreURL: ", a.ConfigStoreURL, "\n"))
	b.WriteString(fmt.Sprint("ConfigDefaultNamespace: ", a.ConfigDefaultNamespace, "\n"))
	b.WriteString(fmt.Sprint("ConfigIdentityAttribute: ", a.ConfigIdentityAttribute, "\n"))
	b.WriteString(fmt.Sprint("ConfigIdentityAttributeDomain: ", a.ConfigIdentityAttributeDomain, "\n"))
	b.WriteString(fmt.Sprintf("LoggingOptions: %#v\n", *a.LoggingOptions))
	b.WriteString(fmt.Sprintf("TracingOptions: %#v\n", *a.TracingOptions))
	return b.String()
}
