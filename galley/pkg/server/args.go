//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"bytes"
	"fmt"
	"time"

	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/probe"
)

const (
	defaultCertDir    = "/etc/istio/certs/"
	defaultCertFile   = defaultCertDir + "cert-chain.pem"
	defaultCACertFile = defaultCertDir + "root-cert.pem"
	defaultCertKey    = defaultCertDir + "key.pem"

	defaultConfigMapFolder = "/etc/istio/config/"
	defaultAccessListFile  = defaultConfigMapFolder + "accesslist.yaml"
)

// Args contains the startup arguments to instantiate Galley.
type Args struct {
	// The path to kube configuration file.
	KubeConfig string

	// resync period to be passed to the K8s machinery.
	ResyncPeriod time.Duration

	// Address to use for Galley's gRPC API.
	APIAddress string

	// Enables gRPC-level tracing
	EnableGRPCTracing bool

	// Maximum size of individual received gRPC messages
	MaxReceivedMessageSize uint

	// Maximum number of outstanding RPCs per connection
	MaxConcurrentStreams uint

	// Insecure gRPC service is used for the MCP server. CertificateFile and KeyFile is ignored.
	Insecure bool

	// CertificateFile to use for mTLS gRPC.
	CertificateFile string

	// KeyFile to use for mTLS gRPC.
	KeyFile string

	// CACertificateFile is the trusted root certificate authority's cert file.
	CACertificateFile string

	// AccessListFile is the YAML file that specifies ids of the allowed mTLS peers.
	AccessListFile string

	// The logging options to use
	LoggingOptions *log.Options

	// The path to the file which indicates the liveness of the server by its existence.
	// This will be used for k8s liveness probe. If empty, it does nothing.
	LivenessProbeOptions *probe.Options

	// The path to the file for readiness probe, similar to LivenessProbePath.
	ReadinessProbeOptions *probe.Options

	// The introspection options to use
	IntrospectionOptions *ctrlz.Options
}

// DefaultArgs allocates an Args struct initialized with Mixer's default configuration.
func DefaultArgs() *Args {
	return &Args{
		APIAddress:             "tcp://0.0.0.0:9901",
		MaxReceivedMessageSize: 1024 * 1024,
		MaxConcurrentStreams:   1024,
		Insecure:               false,
		CertificateFile:        defaultCertFile,
		KeyFile:                defaultCertKey,
		CACertificateFile:      defaultCACertFile,
		AccessListFile:         defaultAccessListFile,
		LoggingOptions:         log.DefaultOptions(),
		LivenessProbeOptions:   &probe.Options{},
		ReadinessProbeOptions:  &probe.Options{},
		IntrospectionOptions:   ctrlz.DefaultOptions(),
	}
}

// String produces a stringified version of the arguments for debugging.
func (a *Args) String() string {
	buf := &bytes.Buffer{}

	fmt.Fprintf(buf, "KubeConfig: %s\n", a.KubeConfig)
	fmt.Fprintf(buf, "ResyncPeriod: %v\n", a.ResyncPeriod)
	fmt.Fprintf(buf, "APIAddress: %s\n", a.APIAddress)
	fmt.Fprintf(buf, "EnableGrpcTracing: %v\n", a.APIAddress)
	fmt.Fprintf(buf, "MaxReceivedMessageSize: %d\n", a.MaxReceivedMessageSize)
	fmt.Fprintf(buf, "MaxConcurrentStreams: %d\n", a.MaxConcurrentStreams)
	fmt.Fprintf(buf, "Insecure: %v\n", a.Insecure)
	fmt.Fprintf(buf, "KeyFile: %s\n", a.KeyFile)
	fmt.Fprintf(buf, "CertificateFile: %s\n", a.CertificateFile)
	fmt.Fprintf(buf, "CACertificateFile: %s\n", a.CACertificateFile)
	fmt.Fprintf(buf, "AccessListFile: %s\n", a.AccessListFile)
	fmt.Fprintf(buf, "LoggingOptions: %#v\n", *a.LoggingOptions)
	fmt.Fprintf(buf, "LivenessProbeOptions: %#v\n", *a.LivenessProbeOptions)
	fmt.Fprintf(buf, "ReadinessProbeOptions: %#v\n", *a.ReadinessProbeOptions)
	fmt.Fprintf(buf, "IntrospectionOptions: %#v\n", *a.IntrospectionOptions)

	return buf.String()
}
