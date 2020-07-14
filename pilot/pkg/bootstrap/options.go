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

package bootstrap

import (
	"time"

	"istio.io/pkg/ctrlz"
	"istio.io/pkg/env"

	"istio.io/istio/pilot/pkg/features"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/keepalive"
)

// RegistryOptions provide configuration options for the configuration controller. If FileDir is set, that directory will
// be monitored for CRD yaml files and will update the controller as those files change (This is used for testing
// purposes). Otherwise, a CRD client is created based on the configuration.
type RegistryOptions struct {
	// If FileDir is set, the below kubernetes options are ignored
	FileDir string

	Registries []string

	// Kubernetes controller options
	KubeOptions kubecontroller.Options
	// ClusterRegistriesNamespace specifies where the multi-cluster secret resides
	ClusterRegistriesNamespace string
	KubeConfig                 string

	// Consul options
	ConsulServerAddr string

	// DistributionTracking control
	DistributionCacheRetention time.Duration

	// DistributionTracking control
	DistributionTrackingEnabled bool
}

// PilotArgs provides all of the configuration parameters for the Pilot discovery service.
type PilotArgs struct {
	ServerOptions      DiscoveryServerOptions
	InjectionOptions   InjectionOptions
	PodName            string
	Namespace          string
	Revision           string
	MeshConfigFile     string
	NetworksConfigFile string
	RegistryOptions    RegistryOptions
	CtrlZOptions       *ctrlz.Options
	Plugins            []string
	MCPOptions         MCPOptions
	KeepaliveOptions   *keepalive.Options
	ShutdownDuration   time.Duration
}

// DiscoveryServerOptions contains options for create a new discovery server instance.
type DiscoveryServerOptions struct {
	// The listening address for HTTP (debug). If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	HTTPAddr string

	// The listening address for HTTPS (webhooks). If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	HTTPSAddr string

	// The listening address for gRPC. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	GRPCAddr string

	// The listening address for the monitoring port. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	MonitoringAddr string

	EnableProfiling bool

	// Optional TLS configuration
	TLSOptions TLSOptions

	// The listening address for secured gRPC. If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	SecureGRPCAddr string
}

type InjectionOptions struct {
	// Directory of injection related config files.
	InjectionDirectory string
}

type MCPOptions struct {
	MaxMessageSize        int
	InitialWindowSize     int
	InitialConnWindowSize int
}

// Optional TLS parameters for Istiod server.
type TLSOptions struct {
	CaCertFile string
	CertFile   string
	KeyFile    string
}

var PodNamespaceVar = env.RegisterStringVar("POD_NAMESPACE", constants.IstioSystemNamespace, "")
var podNameVar = env.RegisterStringVar("POD_NAME", "", "")

// RevisionVar is the value of the Istio control plane revision, e.g. "canary",
// and is the value used by the "istio.io/rev" label.
var RevisionVar = env.RegisterStringVar("REVISION", "", "")

// NewPilotArgs constructs pilotArgs with default values.
func NewPilotArgs(initFuncs ...func(*PilotArgs)) *PilotArgs {
	p := &PilotArgs{}

	// Apply Default Values.
	p.applyDefaults()

	// Apply custom initialization functions.
	for _, fn := range initFuncs {
		fn(p)
	}

	// Set the ClusterRegistries namespace based on the selected namespace.
	if p.Namespace != "" {
		p.RegistryOptions.ClusterRegistriesNamespace = p.Namespace
	} else {
		p.RegistryOptions.ClusterRegistriesNamespace = constants.IstioSystemNamespace
	}

	return p
}

// Apply default value to PilotArgs
func (p *PilotArgs) applyDefaults() {
	p.Namespace = PodNamespaceVar.Get()
	p.PodName = podNameVar.Get()
	p.Revision = RevisionVar.Get()
	p.KeepaliveOptions = keepalive.DefaultOption()
	p.RegistryOptions.DistributionTrackingEnabled = features.EnableDistributionTracking
	p.RegistryOptions.DistributionCacheRetention = features.DistributionHistoryRetention
}
