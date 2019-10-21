// Copyright 2018 Istio Authors
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

package settings

import (
	"bytes"
	"fmt"
	"time"

	"istio.io/istio/galley/pkg/config/util/kuberesource"
	"istio.io/istio/galley/pkg/crd/validation"
	"istio.io/istio/pkg/keepalive"
	"istio.io/istio/pkg/mcp/creds"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/probe"
)

const (
	defaultProbeCheckInterval     = 2 * time.Second
	defaultLivenessProbeFilePath  = "/healthLiveness"
	defaultReadinessProbeFilePath = "/healthReadiness"

	defaultConfigMapFolder  = "/etc/config/"
	defaultMeshConfigFolder = "/etc/mesh-config/"
	defaultAccessListFile   = defaultConfigMapFolder + "accesslist.yaml"
	defaultMeshConfigFile   = defaultMeshConfigFolder + "mesh"
	defaultDomainSuffix     = "cluster.local"
)

// Args contains the startup arguments to instantiate Galley.
type Args struct { // nolint:maligned
	// The path to kube configuration file.
	KubeConfig string

	// resync period to be passed to the K8s machinery.
	ResyncPeriod time.Duration

	// Address to use for Galley's gRPC API.
	APIAddress string

	// Maximum size of individual received gRPC messages
	MaxReceivedMessageSize uint

	// Maximum number of outstanding RPCs per connection
	MaxConcurrentStreams uint

	// Initial Window Size for gRPC connections
	InitialWindowSize uint

	// Initial Connection Window Size for gRPC connections
	InitialConnectionWindowSize uint

	// The credential options to use for MCP.
	CredentialOptions *creds.Options

	// The introspection options to use
	IntrospectionOptions *ctrlz.Options

	// AccessListFile is the YAML file that specifies ids of the allowed mTLS peers.
	AccessListFile string

	// ConfigPath is the path for Galley specific config files
	ConfigPath string

	// ExcludedResourceKinds is a list of resource kinds for which no source events will be triggered.
	// DEPRECATED
	ExcludedResourceKinds []string

	// MeshConfigFile is the path for mesh config
	MeshConfigFile string

	// DNS Domain suffix to use while constructing Ingress based resources.
	DomainSuffix string

	// SinkAddress should be set to the address of a MCP Resource
	// Sink service that Galley will dial out to. Leaving empty disables
	// sink.
	SinkAddress string

	// SinkAuthMode should be set to a name of an authentication plugin,
	// see the istio.io/istio/galley/pkg/autplugins package.
	SinkAuthMode string

	// SinkMeta list of key=values to attach as gRPC stream metadata to
	// outgoing Sink connections.
	SinkMeta []string

	// Enables gRPC-level tracing
	EnableGRPCTracing bool

	// Insecure gRPC service is used for the MCP server. CertificateFile and KeyFile is ignored.
	Insecure bool

	// Enable galley server mode
	EnableServer bool

	// Enable service discovery / endpoint processing.
	EnableServiceDiscovery bool

	// Enable Config Analysis service, that will analyze and update CRD status. UseOldProcessor must be set to false.
	EnableConfigAnalysis bool

	// DisableResourceReadyCheck disables the CRD readiness check. This
	// allows Galley to start when not all supported CRD are
	// registered with the kube-apiserver.
	// DEPRECATED
	DisableResourceReadyCheck bool

	// WatchConfigFiles if set to true, enables Fsnotify watcher for watching and signaling config file changes.
	// Default is false
	WatchConfigFiles bool

	// keep-alive options for the MCP gRPC Server.
	KeepAlive *keepalive.Options

	ValidationArgs *validation.WebhookParameters

	Liveness        probe.Options
	Readiness       probe.Options
	MonitoringPort  uint
	EnableProfiling bool
	PprofPort       uint

	UseOldProcessor bool
}

// DefaultArgs allocates an Args struct initialized with Galley's default configuration.
func DefaultArgs() *Args {
	return &Args{
		ResyncPeriod:                0,
		KubeConfig:                  "",
		APIAddress:                  "tcp://0.0.0.0:9901",
		MaxReceivedMessageSize:      1024 * 1024,
		MaxConcurrentStreams:        1024,
		InitialWindowSize:           1024 * 1024,
		InitialConnectionWindowSize: 1024 * 1024 * 16,
		IntrospectionOptions:        ctrlz.DefaultOptions(),
		Insecure:                    false,
		AccessListFile:              defaultAccessListFile,
		MeshConfigFile:              defaultMeshConfigFile,
		EnableServer:                true,
		CredentialOptions:           creds.DefaultOptions(),
		ConfigPath:                  "",
		DomainSuffix:                defaultDomainSuffix,
		DisableResourceReadyCheck:   false,
		ExcludedResourceKinds:       kuberesource.DefaultExcludedResourceKinds(),
		SinkMeta:                    make([]string, 0),
		KeepAlive:                   keepalive.DefaultOption(),
		ValidationArgs:              validation.DefaultArgs(),
		MonitoringPort:              15014,
		EnableProfiling:             false,
		PprofPort:                   9094,
		UseOldProcessor:             false,
		WatchConfigFiles:            false,
		EnableConfigAnalysis:        false,
		Liveness: probe.Options{
			Path:           defaultLivenessProbeFilePath,
			UpdateInterval: defaultProbeCheckInterval,
		},
		Readiness: probe.Options{
			Path:           defaultReadinessProbeFilePath,
			UpdateInterval: defaultProbeCheckInterval,
		},
	}
}

// String produces a stringified version of the arguments for debugging.
func (a *Args) String() string {
	buf := &bytes.Buffer{}

	_, _ = fmt.Fprintf(buf, "KubeConfig: %s\n", a.KubeConfig)
	_, _ = fmt.Fprintf(buf, "ResyncPeriod: %v\n", a.ResyncPeriod)
	_, _ = fmt.Fprintf(buf, "APIAddress: %s\n", a.APIAddress)
	_, _ = fmt.Fprintf(buf, "EnableGrpcTracing: %v\n", a.EnableGRPCTracing)
	_, _ = fmt.Fprintf(buf, "MaxReceivedMessageSize: %d\n", a.MaxReceivedMessageSize)
	_, _ = fmt.Fprintf(buf, "MaxConcurrentStreams: %d\n", a.MaxConcurrentStreams)
	_, _ = fmt.Fprintf(buf, "InitialWindowSize: %v\n", a.InitialWindowSize)
	_, _ = fmt.Fprintf(buf, "InitialConnectionWindowSize: %v\n", a.InitialConnectionWindowSize)
	_, _ = fmt.Fprintf(buf, "IntrospectionOptions: %+v\n", *a.IntrospectionOptions)
	_, _ = fmt.Fprintf(buf, "Insecure: %v\n", a.Insecure)
	_, _ = fmt.Fprintf(buf, "AccessListFile: %s\n", a.AccessListFile)
	_, _ = fmt.Fprintf(buf, "EnableServer: %v\n", a.EnableServer)
	_, _ = fmt.Fprintf(buf, "KeyFile: %s\n", a.CredentialOptions.KeyFile)
	_, _ = fmt.Fprintf(buf, "CertificateFile: %s\n", a.CredentialOptions.CertificateFile)
	_, _ = fmt.Fprintf(buf, "CACertificateFile: %s\n", a.CredentialOptions.CACertificateFile)
	_, _ = fmt.Fprintf(buf, "ConfigFilePath: %s\n", a.ConfigPath)
	_, _ = fmt.Fprintf(buf, "MeshConfigFile: %s\n", a.MeshConfigFile)
	_, _ = fmt.Fprintf(buf, "DomainSuffix: %s\n", a.DomainSuffix)
	_, _ = fmt.Fprintf(buf, "DisableResourceReadyCheck: %v\n", a.DisableResourceReadyCheck)
	_, _ = fmt.Fprintf(buf, "ExcludedResourceKinds: %v\n", a.ExcludedResourceKinds)
	_, _ = fmt.Fprintf(buf, "SinkAddress: %v\n", a.SinkAddress)
	_, _ = fmt.Fprintf(buf, "SinkAuthMode: %v\n", a.SinkAuthMode)
	_, _ = fmt.Fprintf(buf, "SinkMeta: %v\n", a.SinkMeta)
	_, _ = fmt.Fprintf(buf, "KeepAlive.MaxServerConnectionAge: %v\n", a.KeepAlive.MaxServerConnectionAge)
	_, _ = fmt.Fprintf(buf, "KeepAlive.MaxServerConnectionAgeGrace: %v\n", a.KeepAlive.MaxServerConnectionAgeGrace)
	_, _ = fmt.Fprintf(buf, "KeepAlive.Time: %v\n", a.KeepAlive.Time)
	_, _ = fmt.Fprintf(buf, "KeepAlive.Timeout: %v\n", a.KeepAlive.Timeout)

	return buf.String()
}
