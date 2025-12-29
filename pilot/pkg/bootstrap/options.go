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
	"crypto/tls"
	"fmt"
	"time"

	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/ctrlz"
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/keepalive"
	"istio.io/istio/pkg/kube/krt"
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
}

// PilotArgs provides all of the configuration parameters for the Pilot discovery service.
type PilotArgs struct {
	ServerOptions      DiscoveryServerOptions
	InjectionOptions   InjectionOptions
	PodName            string
	Namespace          string
	CniNamespace       string
	Revision           string
	MeshConfigFile     string
	NetworksConfigFile string
	RegistryOptions    RegistryOptions
	CtrlZOptions       *ctrlz.Options
	KrtDebugger        *krt.DebugHandler `json:"-"`
	KeepaliveOptions   *keepalive.Options
	ShutdownDuration   time.Duration
	JwtRule            string
}

// DiscoveryServerOptions contains options for create a new discovery server instance.
type DiscoveryServerOptions struct {
	// The listening address for HTTP (debug). If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	HTTPAddr string

	// The listening address for HTTPS (webhooks). If the port in the address is empty or "0" (as in "127.0.0.1:" or "[::1]:0")
	// a port number is automatically chosen.
	// If the address is empty, the secure port is disabled, and the
	// webhooks are registered on the HTTP port - a gateway in front will
	// terminate TLS instead.
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

// TLSOptions is optional TLS parameters for Istiod server.
type TLSOptions struct {
	// CaCertFile and related are set using CLI flags.
	CaCertFile      string
	CertFile        string
	KeyFile         string
	TLSCipherSuites []string
	CipherSuites    []uint16 // This is the parsed cipher suites
}

var (
	PodNamespace = env.Register("POD_NAMESPACE", constants.IstioSystemNamespace, "").Get()
	PodName      = env.Register("POD_NAME", "", "").Get()
	JwtRule      = env.Register("JWT_RULE", "",
		"The JWT rule used by istiod authentication").Get()
)

// Revision is the value of the Istio control plane revision, e.g. "canary",
// and is the value used by the "istio.io/rev" label.
var Revision = env.Register("REVISION", "", "").Get()

// NewPilotArgs constructs pilotArgs with default values.
func NewPilotArgs(initFuncs ...func(*PilotArgs)) *PilotArgs {
	p := &PilotArgs{}

	// Apply Default Values.
	p.applyDefaults()

	// Apply custom initialization functions.
	for _, fn := range initFuncs {
		fn(p)
	}

	return p
}

// Apply default value to PilotArgs
func (p *PilotArgs) applyDefaults() {
	p.Namespace = PodNamespace
	p.CniNamespace = PodNamespace
	p.PodName = PodName
	p.Revision = Revision
	p.RegistryOptions.KubeOptions.Revision = Revision
	p.JwtRule = JwtRule
	p.KeepaliveOptions = keepalive.DefaultOption()
	p.RegistryOptions.ClusterRegistriesNamespace = p.Namespace
	p.KrtDebugger = new(krt.DebugHandler)
}

func (p *PilotArgs) Complete() error {
	cipherSuites, err := TLSCipherSuites(p.ServerOptions.TLSOptions.TLSCipherSuites)
	if err != nil {
		return err
	}
	p.ServerOptions.TLSOptions.CipherSuites = cipherSuites
	return nil
}

func allCiphers() map[string]uint16 {
	acceptedCiphers := make(map[string]uint16, len(tls.CipherSuites())+len(tls.InsecureCipherSuites()))
	for _, cipher := range tls.InsecureCipherSuites() {
		acceptedCiphers[cipher.Name] = cipher.ID
	}
	for _, cipher := range tls.CipherSuites() {
		acceptedCiphers[cipher.Name] = cipher.ID
	}
	return acceptedCiphers
}

// TLSCipherSuites returns a list of cipher suite IDs from the cipher suite names passed.
func TLSCipherSuites(cipherNames []string) ([]uint16, error) {
	if len(cipherNames) == 0 {
		return nil, nil
	}
	ciphersIntSlice := make([]uint16, 0)
	possibleCiphers := allCiphers()
	for _, cipher := range cipherNames {
		intValue, ok := possibleCiphers[cipher]
		if !ok {
			return nil, fmt.Errorf("cipher suite %s not supported or doesn't exist", cipher)
		}
		ciphersIntSlice = append(ciphersIntSlice, intValue)
	}
	return ciphersIntSlice, nil
}
