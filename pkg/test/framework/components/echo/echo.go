// Copyright 2019 Istio Authors
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
	"testing"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/util/retry"
)

// Instance is a component that provides access to a deployed echo service.
type Instance interface {
	resource.Resource

	// Config returns the configuration of the Echo instance.
	Config() Config

	// Address of the service (e.g. Kubernetes cluster IP). May be "" if headless.
	Address() string

	// WaitUntilReady waits until this instance is up and ready to receive traffic. If
	// outbound are specified, the wait also includes readiness for each
	// outbound instance as well as waiting for receipt of outbound Envoy configuration
	// (i.e. clusters, routes, listeners from Pilot) in order to enable outbound
	// communication from this instance to each instance in the list.
	WaitUntilReady(outbound ...Instance) error
	WaitUntilReadyOrFail(t testing.TB, outbound ...Instance)

	// Workloads retrieves the list of all deployed workloads for this Echo service.
	// Guarantees at least one workload, if error == nil.
	Workloads() ([]Workload, error)
	WorkloadsOrFail(t testing.TB) []Workload

	// Call makes a call from this Instance to a target Instance.
	Call(options CallOptions) (client.ParsedResponses, error)
	CallOrFail(t testing.TB, options CallOptions) client.ParsedResponses
}

// Port exposed by an Echo Instance
type Port struct {
	// Name of this port
	Name string

	// Protocol to be used for the port.
	Protocol model.Protocol

	// ServicePort number where the service can be reached. Does not necessarily
	// map to the corresponding port numbers for the instances behind the
	// service.
	ServicePort int

	// InstancePort number where this instance is listening for connections.
	// This need not be the same as the ServicePort where the service is accessed.
	InstancePort int
}

// Workload provides an interface for a single deployed echo server.
type Workload interface {
	// Address returns the network address of the endpoint.
	Address() string

	// Sidecar if one was specified.
	Sidecar() Sidecar
}

// Sidecar provides an interface to execute queries against a single Envoy sidecar.
type Sidecar interface {
	// NodeID returns the node ID used for uniquely identifying this sidecar to Pilot.
	NodeID() string

	// Info about the Envoy instance.
	Info() (*envoyAdmin.ServerInfo, error)
	InfoOrFail(t testing.TB) *envoyAdmin.ServerInfo

	// Config of the Envoy instance.
	Config() (*envoyAdmin.ConfigDump, error)
	ConfigOrFail(t testing.TB) *envoyAdmin.ConfigDump

	// WaitForConfig queries the Envoy configuration an executes the given accept handler. If the
	// response is not accepted, the request will be retried until either a timeout or a response
	// has been accepted.
	WaitForConfig(accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option) error
	WaitForConfigOrFail(t testing.TB, accept func(*envoyAdmin.ConfigDump) (bool, error), options ...retry.Option)
}
