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

	"istio.io/istio/pilot/pkg/features"
	kubecontroller "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/keepalive"
	"istio.io/pkg/ctrlz"
	"istio.io/pkg/env"
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
	CaCertFile      string
	CertFile        string
	KeyFile         string
	TLSMinVersion   string
	TLSMaxVersion   string
	TLSCipherSuites []string
	TLSECDHCurves   []string
	// Parsed settings
	MinVersion   uint16
	MaxVersion   uint16
	CipherSuites []uint16
	ECDHCurves   []tls.CurveID
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
	p.PodName = PodName
	p.Revision = Revision
	p.JwtRule = JwtRule
	p.KeepaliveOptions = keepalive.DefaultOption()
	p.RegistryOptions.DistributionTrackingEnabled = features.EnableDistributionTracking
	p.RegistryOptions.DistributionCacheRetention = features.DistributionHistoryRetention
	p.RegistryOptions.ClusterRegistriesNamespace = p.Namespace
}

func (p *PilotArgs) Complete() error {
	minVersion, err := TLSVersion(p.ServerOptions.TLSOptions.TLSMinVersion)
	if err != nil {
		return err
	}
	p.ServerOptions.TLSOptions.MinVersion = minVersion

	maxVersion, err := TLSVersion(p.ServerOptions.TLSOptions.TLSMaxVersion)
	if err != nil {
		return err
	}
	p.ServerOptions.TLSOptions.MaxVersion = maxVersion

	cipherSuites, err := TLSCipherSuites(p.ServerOptions.TLSOptions.TLSCipherSuites)
	if err != nil {
		return err
	}
	p.ServerOptions.TLSOptions.CipherSuites = cipherSuites

	ecdhCurves, err := TLSECDHCurves(p.ServerOptions.TLSOptions.TLSECDHCurves)
	if err != nil {
		return err
	}
	p.ServerOptions.TLSOptions.ECDHCurves = ecdhCurves

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

// TLSECDHCurves returns a list of ECDH curve IDs from the curve names passed.
func TLSECDHCurves(ecdhCurves []string) ([]tls.CurveID, error) {
	if len(ecdhCurves) == 0 {
		return nil, nil
	}
	ecdhCurveIDs := make([]tls.CurveID, 0)
	allowedCurves := map[string]tls.CurveID{
		"P-256":  tls.CurveP256,
		"P-384":  tls.CurveP384,
		"P-521":  tls.CurveP521,
		"X25519": tls.X25519,
	}
	for _, curve := range ecdhCurves {
		curveID, ok := allowedCurves[curve]
		if !ok {
			return nil, fmt.Errorf("ECDH curve %s is not supported or does not exist", curve)
		}
		ecdhCurveIDs = append(ecdhCurveIDs, curveID)
	}
	return ecdhCurveIDs, nil
}

// TLSVersion returns an uint16 code of a given TLS version in string format.
func TLSVersion(tlsVersion string) (uint16, error) {
	switch tlsVersion {
	case "TLSv1_0":
		return tls.VersionTLS10, nil
	case "TLSv1_1":
		return tls.VersionTLS11, nil
	case "TLSv1_2":
		return tls.VersionTLS12, nil
	case "TLSv1_3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("TLS version %s is not supported or does not exist", tlsVersion)
	}
}
