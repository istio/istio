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

package echo

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mitchellh/copystructure"
	"sigs.k8s.io/yaml"

	"istio.io/api/annotation"
	"istio.io/api/label"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
)

// Cluster that can deploy echo instances.
// TODO putting this here for now to deal with circular imports, needs to be moved
type Cluster interface {
	cluster.Cluster

	CanDeploy(Config) (Config, bool)
}

// Configurable is and object that has Config.
type Configurable interface {
	Config() Config

	// ServiceName is the name of this service within the namespace.
	ServiceName() string

	// NamespaceName returns the name of the namespace or "" if the Namespace is nil.
	NamespaceName() string

	// NamespacedName returns the namespaced name for this service.
	// Short form for Config().NamespacedName().
	NamespacedName() NamespacedName

	// SpiffeIdentity returns the spiffe identity for this service (without the spiffe:// prefix)
	SpiffeIdentity() string

	// ClusterLocalFQDN returns the fully qualified domain name for cluster-local host.
	ClusterLocalFQDN() string

	// ClusterSetLocalFQDN returns the fully qualified domain name for the Kubernetes
	// Multi-Cluster Services (MCS) Cluster Set host.
	ClusterSetLocalFQDN() string

	// PortForName is a short form for Config().Ports.MustForName().
	PortForName(name string) Port
}

type VMDistro = string

const (
	UbuntuBionic VMDistro = "UbuntuBionic"
	UbuntuNoble  VMDistro = "UbuntuNoble"
	Debian12     VMDistro = "Debian12"
	Rockylinux9  VMDistro = "Rockylinux9"

	DefaultVMDistro = UbuntuNoble
)

// Config defines the options for creating an Echo component.
// nolint: maligned
type Config struct {
	// Namespace of the echo Instance. If not provided, a default namespace "apps" is used.
	Namespace namespace.Instance

	// DefaultHostHeader overrides the default Host header for calls (`service.namespace.svc.cluster.local`)
	DefaultHostHeader string

	// Domain of the echo Instance. If not provided, a default will be selected.
	Domain string

	// Service indicates the service name of the Echo application.
	Service string

	// Version indicates the version path for calls to the Echo application.
	Version string

	// Locality (k8s only) indicates the locality of the deployed app.
	Locality string

	// Headless (k8s only) indicates that no ClusterIP should be specified.
	Headless bool

	// StatefulSet indicates that the pod should be backed by a StatefulSet. This implies Headless=true
	// as well.
	StatefulSet bool

	// StaticAddress for some echo implementations is an address locally reachable within
	// the test framework and from the echo Cluster's network.
	StaticAddresses []string

	// ServiceAccount (k8s only) indicates that a service account should be created
	// for the deployment.
	ServiceAccount bool

	// DisableAutomountSAToken indicates to opt out of auto mounting ServiceAccount's API credentials
	DisableAutomountSAToken bool

	// Ports for this application. Port numbers may or may not be used, depending
	// on the implementation.
	Ports Ports

	// ServiceAnnotations is annotations on service object.
	ServiceAnnotations map[string]string

	// ServiceLabels is the labels on service object.
	ServiceLabels map[string]string

	// ReadinessTimeout specifies the timeout that we wait the application to
	// become ready.
	ReadinessTimeout time.Duration

	// ReadinessTCPPort if set, use this port for the TCP readiness probe (instead of using a HTTP probe).
	ReadinessTCPPort string

	// ReadinessGRPCPort if set, use this port for the GRPC readiness probe (instead of using a HTTP probe).
	ReadinessGRPCPort string

	// Subsets contains the list of Subsets config belonging to this echo
	// service instance.
	Subsets []SubsetConfig

	// Cluster to be used in a multicluster environment
	Cluster cluster.Cluster

	// TLS settings for echo server
	TLSSettings *common.TLSSettings

	// If enabled, echo will be deployed as a "VM". This means it will run Envoy in the same pod as echo,
	// disable sidecar injection, etc.
	// This aims to simulate a VM, but instead of managing the complex test setup of spinning up a VM,
	// connecting, etc we run it inside a pod. The pod has pretty much all Kubernetes features disabled (DNS and SA token mount)
	// such that we can adequately simulate a VM and DIY the bootstrapping.
	DeployAsVM bool

	// If enabled, ISTIO_META_AUTO_REGISTER_GROUP will be set on the VM and the WorkloadEntry will be created automatically.
	AutoRegisterVM bool

	// The distro to use for a VM. For fake VMs, this maps to docker images.
	VMDistro VMDistro

	// The set of environment variables to set for `DeployAsVM` instances.
	VMEnvironment map[string]string

	// If enabled, an additional ext-authz container will be included in the deployment. This is mainly used to test
	// the CUSTOM authorization policy when the ext-authz server is deployed locally with the application container in
	// the same pod.
	IncludeExtAuthz bool

	// IPFamily for the service. This is optional field. Mainly is used for dual stack testing
	IPFamilies string

	// IPFamilyPolicy. This is optional field. Mainly is used for dual stack testing.
	IPFamilyPolicy string

	// This is an optional field used as part of dual stack testing.
	DualStack bool

	// ServiceWaypointProxy specifies if this workload should have an associated Waypoint for service-addressed traffic
	ServiceWaypointProxy string

	// WorkloadWaypointProxy specifies if this workload should have an associated Waypoint for workload-addressed traffic
	WorkloadWaypointProxy string

	// Specify IP family to which echo will bind
	BindFamily string
}

// Getter for a custom echo deployment
type ConfigGetter func() []Config

// Get is a utility method that helps in readability of call sites.
func (g ConfigGetter) Get() []Config {
	return g()
}

// Future creates a Getter for a variable the custom echo deployment that will be set at sometime in the future.
// This is helpful for configuring a setup chain for a test suite that operates on global variables.
func ConfigFuture(custom *[]Config) ConfigGetter {
	return func() []Config {
		return *custom
	}
}

// NamespaceName returns the string name of the namespace.
func (c Config) NamespaceName() string {
	return c.NamespacedName().NamespaceName()
}

// NamespacedName returns the namespaced name for the service.
func (c Config) NamespacedName() NamespacedName {
	return NamespacedName{
		Name:      c.Service,
		Namespace: c.Namespace,
	}
}

func (c Config) AccountName() string {
	if c.ServiceAccount {
		return c.Service
	}
	return "default"
}

// SpiffeIdentity returns the service account name for this service.
func (c Config) SpiffeIdentity() string {
	return "cluster.local/ns/" + c.NamespaceName() + "/sa/" + c.Service
}

// SubsetConfig is the config for a group of Subsets (e.g. Kubernetes deployment).
type SubsetConfig struct {
	// The version of the deployment.
	Version string
	// Annotations provides metadata hints for deployment of the instance.
	Annotations map[string]string
	// Labels provides metadata hints for deployment of the instance.
	Labels map[string]string
	// Replicas of this deployment
	Replicas int

	// TODO: port more into workload config.
}

// String implements the Configuration interface (which implements fmt.Stringer)
func (c Config) String() string {
	return fmt.Sprint("{service: ", c.Service, ", version: ", c.Version, "}")
}

// ClusterLocalFQDN returns the fully qualified domain name for cluster-local host.
func (c Config) ClusterLocalFQDN() string {
	out := c.Service
	if c.Namespace != nil {
		out += "." + c.Namespace.Name() + ".svc"
	} else {
		out += ".default.svc"
	}
	if c.Domain != "" {
		out += "." + c.Domain
	}
	return out
}

// ClusterSetLocalFQDN returns the fully qualified domain name for the Kubernetes
// Multi-Cluster Services (MCS) Cluster Set host.
func (c Config) ClusterSetLocalFQDN() string {
	out := c.Service
	if c.Namespace != nil {
		out += "." + c.Namespace.Name() + ".svc"
	} else {
		out += ".default.svc"
	}
	out += "." + constants.DefaultClusterSetLocalDomain
	return out
}

// HostnameVariants for a Kubernetes service.
// Results may be invalid for non k8s.
func (c Config) HostnameVariants() []string {
	ns := c.NamespaceName()
	if ns == "" {
		ns = "default"
	}
	return []string{
		c.Service,
		c.Service + "." + ns,
		c.Service + "." + ns + ".svc",
		c.ClusterLocalFQDN(),
	}
}

// HostHeader returns the Host header that will be used for calls to this service.
func (c Config) HostHeader() string {
	if c.DefaultHostHeader != "" {
		return c.DefaultHostHeader
	}
	return c.ClusterLocalFQDN()
}

func (c Config) IsHeadless() bool {
	return c.Headless
}

func (c Config) IsStatefulSet() bool {
	return c.StatefulSet
}

// IsNaked checks if the config has no sidecar.
// Note: instances that mix subsets with and without sidecars are considered 'naked'.
func (c Config) IsNaked() bool {
	for _, s := range c.Subsets {
		if s.Annotations != nil && s.Annotations[annotation.SidecarInject.Name] == "false" {
			// Sidecar injection is disabled - it's naked.
			return true
		}
	}
	return false
}

// IsAllNaked checks if every subset is configured with no sidecar.
func (c Config) IsAllNaked() bool {
	if len(c.Subsets) == 0 {
		// No subsets - default to not-naked.
		return false
	}
	// if ANY subset has a sidecar, not naked.
	for _, s := range c.Subsets {
		if s.Annotations == nil || s.Annotations[annotation.SidecarInject.Name] != "false" {
			// Sidecar injection is enabled - it's not naked.
			return false
		}
	}
	// All subsets were annotated indicating no sidecar injection.
	return true
}

func (c Config) IsProxylessGRPC() bool {
	// TODO make these check if any subset has a matching annotation
	return len(c.Subsets) > 0 && c.Subsets[0].Annotations != nil && strings.HasPrefix(c.Subsets[0].Annotations[annotation.InjectTemplates.Name], "grpc-")
}

func (c Config) IsTProxy() bool {
	// TODO this could be HasCustomInjectionMode
	return len(c.Subsets) > 0 && c.Subsets[0].Annotations != nil && c.Subsets[0].Annotations[annotation.SidecarInterceptionMode.Name] == "TPROXY"
}

func (c Config) HasAnyWaypointProxy() bool {
	return c.ServiceWaypointProxy != "" || c.WorkloadWaypointProxy != ""
}

func (c Config) HasServiceAddressedWaypointProxy() bool {
	return c.ServiceWaypointProxy != ""
}

func (c Config) HasWorkloadAddressedWaypointProxy() bool {
	return c.WorkloadWaypointProxy != ""
}

func (c Config) HasSidecar() bool {
	var perPodEnable, perPodDisable bool
	if len(c.Subsets) > 0 && c.Subsets[0].Labels != nil {
		perPodEnable = c.Subsets[0].Labels["sidecar.istio.io/inject"] == "true"
		perPodDisable = c.Subsets[0].Labels["sidecar.istio.io/inject"] == "false"
	}

	return perPodEnable || (!perPodDisable && c.Namespace != nil && c.Namespace.IsInjected())
}

func (c Config) IsUncaptured() bool {
	// TODO this can be more robust to not require labeling initial echo config (check namespace + isWaypoint + not sidecar)
	return len(c.Subsets) > 0 && c.Subsets[0].Labels != nil && c.Subsets[0].Labels[label.IoIstioDataplaneMode.Name] == constants.DataplaneModeNone
}

func (c Config) HasProxyCapabilities() bool {
	return !c.IsUncaptured() || c.HasSidecar() || c.IsProxylessGRPC()
}

func (c Config) IsAmbient() bool {
	return c.HasProxyCapabilities() && !c.HasSidecar()
}

func (c Config) IsVM() bool {
	return c.DeployAsVM
}

func (c Config) IsSotw() bool {
	// TODO this doesn't hold if delta is off by default
	return len(c.Subsets) > 0 && c.Subsets[0].Annotations != nil && strings.Contains(c.Subsets[0].Annotations[annotation.ProxyConfig.Name], "ISTIO_DELTA_XDS")
}

// IsRegularPod returns true if the echo pod is not any of the following:
// - VM
// - Naked
// - Headless
// - TProxy
// - Multi-Subset
// - DualStack Service Pods
func (c Config) IsRegularPod() bool {
	return len(c.Subsets) == 1 &&
		!c.IsVM() &&
		!c.IsTProxy() &&
		!c.IsNaked() &&
		!c.IsHeadless() &&
		!c.IsStatefulSet() &&
		!c.IsProxylessGRPC() &&
		!c.HasServiceAddressedWaypointProxy() &&
		!c.HasWorkloadAddressedWaypointProxy() &&
		!c.ZTunnelCaptured() &&
		!c.DualStack
}

// WaypointClient means the client supports HBONE and does zTunnel redirection.
// Currently, only zTunnel captured sources do this. Eventually this might be enabled
// for ingress and/or sidecars.
func (c Config) WaypointClient() bool {
	return c.ZTunnelCaptured() && !c.IsUncaptured()
}

// ZTunnelCaptured returns true in ambient enabled namespaces where there is no sidecar
func (c Config) ZTunnelCaptured() bool {
	haveSubsets := len(c.Subsets) > 0
	if c.Namespace.IsAmbient() && haveSubsets &&
		c.Subsets[0].Labels[label.IoIstioDataplaneMode.Name] != constants.DataplaneModeNone &&
		!c.HasSidecar() {
		return true
	}
	return haveSubsets && c.Subsets[0].Annotations[annotation.AmbientRedirection.Name] == constants.AmbientRedirectionEnabled
}

// DeepCopy creates a clone of IstioEndpoint.
func (c Config) DeepCopy() Config {
	newc := c
	newc.Cluster = nil
	newc = copyInternal(newc).(Config)
	newc.Cluster = c.Cluster
	newc.Namespace = c.Namespace
	return newc
}

func (c Config) IsExternal() bool {
	return c.HostHeader() != c.ClusterLocalFQDN()
}

const (
	defaultService   = "echo"
	defaultVersion   = "v1"
	defaultNamespace = "echo"
	defaultDomain    = constants.DefaultClusterLocalDomain
)

func (c *Config) FillDefaults(ctx resource.Context) (err error) {
	if c.Service == "" {
		c.Service = defaultService
	}

	if c.Version == "" {
		c.Version = defaultVersion
	}

	if c.Domain == "" {
		c.Domain = defaultDomain
	}

	if c.VMDistro == "" {
		c.VMDistro = DefaultVMDistro
	}
	if c.StatefulSet {
		// Statefulset requires headless
		c.Headless = true
	}

	// Convert legacy config to workload oritended.
	if c.Subsets == nil {
		c.Subsets = []SubsetConfig{
			{
				Version: c.Version,
			},
		}
	}

	for i := range c.Subsets {
		if c.Subsets[i].Version == "" {
			c.Subsets[i].Version = c.Version
		}
	}
	c.addPortIfMissing(protocol.GRPC)
	// If no namespace was provided, use the default.
	if c.Namespace == nil && ctx != nil {
		nsConfig := namespace.Config{
			Prefix: defaultNamespace,
			Inject: true,
		}
		if c.Namespace, err = namespace.New(ctx, nsConfig); err != nil {
			return err
		}
	}

	// Make a copy of the ports array. This avoids potential corruption if multiple Echo
	// Instances share the same underlying ports array.
	c.Ports = append([]Port{}, c.Ports...)

	// Mark all user-defined ports as used, so the port generator won't assign them.
	portGen := newPortGenerators()
	for _, p := range c.Ports {
		if p.ServicePort > 0 {
			if portGen.Service.IsUsed(p.ServicePort) {
				return fmt.Errorf("failed configuring port %s: service port already used %d", p.Name, p.ServicePort)
			}
			portGen.Service.SetUsed(p.ServicePort)
		}
		if p.WorkloadPort > 0 {
			if portGen.Instance.IsUsed(p.WorkloadPort) {
				return fmt.Errorf("failed configuring port %s: instance port already used %d", p.Name, p.WorkloadPort)
			}
			portGen.Instance.SetUsed(p.WorkloadPort)
		}
	}

	// Second pass: try to make unassigned instance ports match service port.
	for i, p := range c.Ports {
		if p.WorkloadPort == 0 && p.ServicePort > 0 && !portGen.Instance.IsUsed(p.ServicePort) {
			c.Ports[i].WorkloadPort = p.ServicePort
			portGen.Instance.SetUsed(p.ServicePort)
		}
	}

	// Final pass: assign default values for any ports that haven't been specified.
	for i, p := range c.Ports {
		if p.ServicePort == 0 {
			c.Ports[i].ServicePort = portGen.Service.Next(p.Protocol)
		}
		if p.WorkloadPort == 0 {
			c.Ports[i].WorkloadPort = portGen.Instance.Next(p.Protocol)
		}
	}

	// If readiness probe is not specified by a test, wait a long time
	// Waiting forever would cause the test to timeout and lose logs
	if c.ReadinessTimeout == 0 {
		c.ReadinessTimeout = DefaultReadinessTimeout()
	}

	return nil
}

// addPortIfMissing adds a port for the given protocol if none was found.
func (c *Config) addPortIfMissing(protocol protocol.Instance) {
	if _, found := c.Ports.ForProtocol(protocol); !found {
		c.Ports = append([]Port{
			{
				Name:     strings.ToLower(string(protocol)),
				Protocol: protocol,
			},
		}, c.Ports...)
	}
}

func copyInternal(v any) any {
	copied, err := copystructure.Copy(v)
	if err != nil {
		// There are 2 locations where errors are generated in copystructure.Copy:
		//  * The reflection walk over the structure fails, which should never happen
		//  * A configurable copy function returns an error. This is only used for copying times, which never returns an error.
		// Therefore, this should never happen
		panic(err)
	}
	return copied
}

// ParseConfigs unmarshals the given YAML bytes into []Config, using a namespace.Static rather
// than attempting to Claim the configured namespace.
func ParseConfigs(bytes []byte) ([]Config, error) {
	// parse into flexible type, so we can remove Namespace and parse that ourselves
	raw := make([]map[string]any, 0)
	if err := yaml.Unmarshal(bytes, &raw); err != nil {
		return nil, err
	}
	configs := make([]Config, len(raw))

	for i, raw := range raw {
		if ns, ok := raw["Namespace"]; ok {
			configs[i].Namespace = namespace.Static(fmt.Sprint(ns))
			delete(raw, "Namespace")
		}
	}

	// unmarshal again after Namespace stripped is stripped, to avoid unmarshal error
	modifiedBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(modifiedBytes, &configs); err != nil {
		return nil, nil
	}

	return configs, nil
}

// WorkloadClass returns the type of workload a given config is.
func (c Config) WorkloadClass() WorkloadClass {
	if c.IsProxylessGRPC() {
		return Proxyless
	} else if c.IsVM() {
		return VM
	} else if c.IsTProxy() {
		return TProxy
	} else if c.IsNaked() {
		return Naked
	} else if c.IsExternal() {
		return External
	} else if c.IsStatefulSet() {
		return StatefulSet
	} else if c.IsSotw() {
		return Sotw
	} else if c.HasAnyWaypointProxy() {
		return Waypoint
	} else if c.ZTunnelCaptured() && !c.HasAnyWaypointProxy() {
		return Captured
	}
	if c.IsHeadless() {
		return Headless
	}
	return Standard
}
