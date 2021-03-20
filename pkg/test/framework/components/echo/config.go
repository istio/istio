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
	"fmt"
	"time"

	"github.com/mitchellh/copystructure"

	"istio.io/istio/pkg/test/echo/common"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// Cluster that can deploy echo instances.
// TODO putting this here for now to deal with circular imports, needs to be moved
type Cluster interface {
	cluster.Cluster

	CanDeploy(Config) (Config, bool)
}

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

	// The image name to be used to pull the image for the VM. `DeployAsVM` must be enabled.
	VMImage string

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

// FQDN returns the fully qualified domain name for the service.
func (c Config) FQDN() string {
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

// HostHeader returns the Host header that will be used for calls to this service.
func (c Config) HostHeader() string {
	if c.DefaultHostHeader != "" {
		return c.DefaultHostHeader
	}
	return c.FQDN()
}

func (c Config) IsHeadless() bool {
	return c.Headless
}

func (c Config) IsNaked() bool {
	return len(c.Subsets) > 0 && c.Subsets[0].Annotations != nil && !c.Subsets[0].Annotations.GetBool(SidecarInject)
}

func (c Config) IsVM() bool {
	return c.DeployAsVM
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
	return c.HostHeader() != c.FQDN()
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
