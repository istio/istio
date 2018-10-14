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

package environment

import (
	"net/http"
	"testing"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/gogo/googleapis/google/rpc"
	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	prom "github.com/prometheus/common/model"
	corev1 "k8s.io/api/core/v1"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/framework/settings"
)

// AppProtocol enumerates the protocol options for calling an DeployedAppEndpoint endpoint.
type AppProtocol string

const (
	// AppProtocolHTTP calls the app with HTTP
	AppProtocolHTTP = "http"
	// AppProtocolGRPC calls the app with GRPC
	AppProtocolGRPC = "grpc"
	// AppProtocolWebSocket calls the app with WebSocket
	AppProtocolWebSocket = "ws"
)

type (

	// Environment is the public interface for interacting with the testing environment. It provides a
	// convenient facade that allows quickly accessing individual components.
	Environment interface {

		// Configure applies the given configuration to the mesh. The configuration is in Kubernetes style
		// serialized YAML format.
		Configure(tb testing.TB, config string)

		// Evaluate the given template using the current set of template parameters from environment.
		Evaluate(tb testing.TB, template string) string

		// GetMixer returns a deployed Mixer instance in the environment.
		GetMixer() (DeployedMixer, error)

		// GetMixerOrFail returns a deployed Mixer instance in the environment, or fails the test if unsuccessful.
		GetMixerOrFail(t testing.TB) DeployedMixer

		// GetPilot returns a deployed Pilot instance in the environment.
		GetPilot() (DeployedPilot, error)

		// GetPilotOrFail returns a deployed Pilot instance in the environment, or fails the test if unsuccessful.
		GetPilotOrFail(t testing.TB) DeployedPilot

		// GetCitadel returns a deployed Citadel instance in the environment.
		GetCitadel() (DeployedCitadel, error)

		// GetCitadelOrFail returns a deployed Citadel instance in the environment, or fails the test if unsuccessful.
		GetCitadelOrFail(t testing.TB) DeployedCitadel

		// GetAPIServer returns a handle to the ambient API Server in the environment.
		GetAPIServer() (DeployedAPIServer, error)

		// GetAPIServerOrFail returns a handle to the ambient API Server in the environment, or fails the test if
		// unsuccessful.
		GetAPIServerOrFail(t testing.TB) DeployedAPIServer

		// GetApp returns a fake testing app object for the given name.
		GetApp(name string) (DeployedApp, error)

		// GetAppOrFail returns a fake testing app object for the given name, or fails the test if unsuccessful.
		GetAppOrFail(name string, t testing.TB) DeployedApp

		// GetFortioApp returns a Fortio App object for the given name.
		GetFortioApp(name string) (DeployedFortioApp, error)

		// GetFortioAppOrFail returns a Fortio App object for the given name, or fails the test if unsuccessful.
		GetFortioAppOrFail(name string, t testing.TB) DeployedFortioApp

		// TODO: We should rationalize and come up with a single set of GetFortioApp(s) method.
		// See https://github.com/istio/istio/issues/6171.

		// GetFortioApps returns a set of Fortio Apps based on the given selector.
		GetFortioApps(selector string, t testing.TB) []DeployedFortioApp

		// GetPolicyBackendOrFail returns the mock policy backend that is used by Mixer for policy checks and reports.
		GetPolicyBackendOrFail(t testing.TB) DeployedPolicyBackend

		// DeployBookInfo deploys BookInfo into the test namespace.
		DeployBookInfo() error

		// DeployBookInfoOrFail deploys BookInfo into the test namespace, or fails the test if unsuccessful.
		DeployBookInfoOrFail(t testing.TB)

		// GetPrometheus returns a handle to a deployed Prometheus instance.
		GetPrometheus() (DeployedPrometheus, error)

		// GetPrometheusOrFail return a handle to a deployed Prometheus instance, or fails the test if unsuccessful.
		GetPrometheusOrFail(t testing.TB) DeployedPrometheus

		// GetIngress returns the Istio Ingress Gateway.
		GetIngress() (DeployedIngress, error)

		// GetIngressOrFail returns the Istio Ingress Gateway, or fails the test if uncessfull.
		GetIngressOrFail(t testing.TB) DeployedIngress

		// ComponentContext returns an context that can be used to access internal implementation details
		// of the underlying environment.
		ComponentContext() ComponentContext
	}

	// ComponentContext gets passed to individual components and allows them to integrate with the test
	// framework. Tests can access it directly as well, in case they need to break glass.
	ComponentContext interface {
		Settings() settings.Settings
		Environment() Implementation
	}

	// Implementation is a tagging interface for the underlying environment specific implementation (i.e.
	// kubernetes or local environment specific implementation).
	Implementation interface {
		// EnvironmentID is the unique ID of the implemented environment.
		EnvironmentID() settings.EnvironmentID

		// Evaluate the given template with environment specific template variables.
		Evaluate(tmpl string) (string, error)
	}

	// Deployed represents a deployed component
	Deployed interface {
	}

	// AppCallOptions defines options for calling a DeployedAppEndpoint.
	AppCallOptions struct {
		// Secure indicates whether a secure connection should be established to the endpoint.
		Secure bool

		// Protocol indicates the protocol to be used.
		Protocol AppProtocol

		// UseShortHostname indicates whether shortened hostnames should be used. This may be ignored by the environment.
		UseShortHostname bool

		// Count indicates the number of exchanges that should be made with the service endpoint. If not set (i.e. 0), defaults to 1.
		Count int

		// Headers indicates headers that should be sent in the request. Ingnored for WebSocket calls.
		Headers http.Header
	}

	// DeployedApp represents a deployed fake App within the mesh.
	DeployedApp interface {
		Deployed
		Name() string
		Endpoints() []DeployedAppEndpoint
		EndpointsForProtocol(protocol model.Protocol) []DeployedAppEndpoint
		Call(e DeployedAppEndpoint, opts AppCallOptions) ([]*echo.ParsedResponse, error)
		CallOrFail(e DeployedAppEndpoint, opts AppCallOptions, t testing.TB) []*echo.ParsedResponse
	}

	// DeployedPolicyBackend represents a deployed fake policy backend for Mixer.
	DeployedPolicyBackend interface {
		Deployed

		// DenyCheck indicates that the policy backend should deny all incoming check requests when deny is
		// set to true.
		DenyCheck(t testing.TB, deny bool)

		// ExpectReport checks that the backend has received the given report requests. The requests are consumed
		// after the call completes.
		ExpectReport(t testing.TB, expected ...proto.Message)

		// ExpectReportJSON checks that the backend has received the given report request.  The requests are
		// consumed after the call completes.
		ExpectReportJSON(t testing.TB, expected ...string)

		// CreateConfigSnippet for the Mixer adapter to talk to this policy backend.
		// The supplied name will be the name of the handler.
		CreateConfigSnippet(name string) string
	}

	// DeployedAPIServer represents an in-cluster API Server, or the Minikube for the local testing case.
	// Note that this should *NOT* be used to configure components, that should be done through top-level APIs.
	// This is mainly available to trigger non-config related operations, and integration testing of components
	// that are on the config path.
	DeployedAPIServer interface {
		Deployed

		ApplyYaml(yml string) error
	}

	// DeployedAppEndpoint represents a single endpoint in a DeployedApp.
	DeployedAppEndpoint interface {
		Name() string
		Owner() DeployedApp
		Protocol() model.Protocol
	}

	// DeployedMixer represents a deployed Mixer instance.
	DeployedMixer interface {
		Deployed

		// Report is called directly with the given attributes.
		Report(t testing.TB, attributes map[string]interface{})
		Check(t testing.TB, attributes map[string]interface{}) CheckResponse
	}

	// CheckResponse that is returned from a Mixer Check call.
	CheckResponse struct {
		Raw *istio_mixer_v1.CheckResponse
	}

	// DeployedPilot represents a deployed Pilot instance.
	DeployedPilot interface {
		Deployed

		CallDiscovery(req *xdsapi.DiscoveryRequest) (*xdsapi.DiscoveryResponse, error)
	}

	// DeployedFortioApp represents a deployed fake Fortio App within the mesh.
	DeployedFortioApp interface {
		Deployed
		CallFortio(arg string, path string) (FortioAppCallResult, error)
	}

	// FortioAppCallResult provides details about the result of a fortio call
	FortioAppCallResult struct {
		// The raw content of the response
		Raw string
	}

	// DeployedCitadel represents a deployed Citadel instance.
	DeployedCitadel interface {
		Deployed

		WaitForSecretToExist() (*corev1.Secret, error)
		DeleteSecret() error
	}

	// DeployedPrometheus represents a deployed Prometheus instance in a Kubernetes cluster.
	DeployedPrometheus interface {
		Deployed

		// API Returns the core Prometheus APIs.
		API() v1.API

		// WaitForQuiesce runs the provided query periodically until the result gets stable.
		WaitForQuiesce(fmt string, args ...interface{}) (prom.Value, error)

		// WaitForOneOrMore runs the provided query and waits until one (or more for vector) values are available.
		WaitForOneOrMore(fmt string, args ...interface{}) error

		// Sum all the samples that has the given labels in the given vector value.
		Sum(val prom.Value, labels map[string]string) (float64, error)
	}

	// DeployedIngress represents a deployed Ingress Gateway instance.
	DeployedIngress interface {
		Deployed

		// Address returns the external HTTP address of the ingress gateway (or the NodePort address,
		// when running under Minikube).
		Address() string

		//  Call makes an HTTP call through ingress, where the URL has the given path.
		Call(path string) (IngressCallResponse, error)
	}

	// BookInfo represents an optionally deployable bookinfo installation.
	// TODO: This interface is an internal implementation detail, we should find a better home for this.
	BookInfo interface {
		Deploy() error
	}

	// IngressCallResponse is the result of a call made through Istio Ingress.
	IngressCallResponse struct {
		// Response status code
		Code int

		// Response body
		Body string
	}
)

// Succeeded returns true if the precondition check was successful.
func (c *CheckResponse) Succeeded() bool {
	return c.Raw.Precondition.Status.Code == int32(rpc.OK)
}
