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
	"gopkg.in/yaml.v3"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo/echotypes"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// Cluster that can deploy echo instances.
// TODO putting this here for now to deal with circular imports, needs to be moved
type Cluster interface {
	cluster.Cluster

	CanDeploy(Config) (Config, bool)
}

type VMDistro = string

const (
	UbuntuXenial VMDistro = "UbuntuXenial"
	UbuntuFocal  VMDistro = "UbuntuFocal"
	UbuntuBionic VMDistro = "UbuntuBionic"
	Debian9      VMDistro = "Debian9"
	Debian10     VMDistro = "Debian10"
	Centos7      VMDistro = "Centos7"
	Centos8      VMDistro = "Centos8"

	DefaultVMDistro = UbuntuBionic
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

	// Ports for this application. Port numbers may or may not be used, depending
	// on the implementation.
	Ports []Port

	// WorkloadOnlyPorts for ports only defined in the workload but not in the k8s service.
	// This is used to test the inbound pass-through filter chain.
	WorkloadOnlyPorts []WorkloadPort

	// ServiceAnnotations is annotations on service object.
	ServiceAnnotations Annotations

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
}

// SubsetConfig is the config for a group of Subsets (e.g. Kubernetes deployment).
type SubsetConfig struct {
	// The version of the deployment.
	Version string
	// Annotations provides metadata hints for deployment of the instance.
	Annotations Annotations
	// TODO: port more into workload config.
}

// String implements the Configuration interface (which implements fmt.Stringer)
func (c Config) String() string {
	return fmt.Sprint("{service: ", c.Service, ", version: ", c.Version, "}")
}

// PortByName looks up a given port by name
func (c Config) PortByName(name string) *Port {
	for _, p := range c.Ports {
		if p.Name == name {
			return &p
		}
	}
	return nil
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

func (c Config) IsNaked() bool {
	return len(c.Subsets) > 0 && c.Subsets[0].Annotations != nil && !c.Subsets[0].Annotations.GetBool(SidecarInject)
}

func (c Config) IsProxylessGRPC() bool {
	// TODO make these check if any subset has a matching annotation
	return len(c.Subsets) > 0 && c.Subsets[0].Annotations != nil && strings.HasPrefix(c.Subsets[0].Annotations.Get(SidecarInjectTemplates), "grpc-")
}

func (c Config) IsTProxy() bool {
	// TODO this could be HasCustomInjectionMode
	return len(c.Subsets) > 0 && c.Subsets[0].Annotations != nil && c.Subsets[0].Annotations.Get(SidecarInterceptionMode) == "TPROXY"
}

func (c Config) IsVM() bool {
	return c.DeployAsVM
}

func (c Config) IsDelta() bool {
	// TODO this doesn't hold if delta is on by default
	return len(c.Subsets) > 0 && c.Subsets[0].Annotations != nil && strings.Contains(c.Subsets[0].Annotations.Get(SidecarProxyConfig), "ISTIO_DELTA_XDS")
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

func copyInternal(v interface{}) interface{} {
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
	raw := make([]map[string]interface{}, 0)
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

// Class returns the type of workload a given config is.
func (c Config) Class() echotypes.Class {
	if c.IsProxylessGRPC() {
		return echotypes.Proxyless
	} else if c.IsVM() {
		return echotypes.VM
	} else if c.IsTProxy() {
		return echotypes.TProxy
	} else if c.IsNaked() {
		return echotypes.Naked
	} else if c.IsExternal() {
		return echotypes.External
	} else if c.IsStatefulSet() {
		return echotypes.StatefulSet
	} else if c.IsDelta() {
		// TODO remove if delta is on by default
		return echotypes.Delta
	}
	if c.IsHeadless() {
		return echotypes.Headless
	}
	return echotypes.Standard
}
