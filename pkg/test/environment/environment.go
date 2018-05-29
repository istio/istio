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
	"net/url"
	"testing"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/pilot/pkg/model"
)

const (
	httpOK = "200"
)

// Interface is a common interface for all testing environments.
type Interface interface {

	// Configure applies the given configuration to the mesh.
	Configure(tb testing.TB, config string)

	// TODO: Implement Configure overload that can consume config from a directory

	// GetMixer returns a deployed Mixer instance in the environment.
	GetMixer() (DeployedMixer, error)
	// GetMixerOrFail returns a deployed Mixer instance in the environment, or fails the test if unsuccessful.
	GetMixerOrFail(t testing.TB) DeployedMixer

	// GetPilot returns a deployed Pilot instance in the environment.
	GetPilot() (DeployedPilot, error)
	// GetPilotOrFail returns a deployed Pilot instance in the environment, or fails the test if unsuccessful.
	GetPilotOrFail(t testing.TB) DeployedPilot

	// GetApp returns a fake testing app object for the given name.
	GetApp(name string) (DeployedApp, error)
	// GetAppOrFail returns a fake testing app object for the given name, or fails the test if unsuccessful.
	GetAppOrFail(name string, t testing.TB) DeployedApp

	// GetFortioApp returns a Fortio App object for the given name.
	GetFortioApp(name string) (DeployedFortioApp, error)
	// GetFortioAppOrFail returns a Fortio App object for the given name, or fails the test if unsuccessful.
	GetFortioAppOrFail(name string, t testing.TB) DeployedFortioApp

	// TODO: We should remove this overload in favor of the previous two.

	// GetFortioApps returns a set of Fortio Apps based on the given selector.
	GetFortioApps(selector string, t testing.TB) []DeployedFortioApp

	// GetPolicyBackendOrFail returns the mock policy backend that is used by Mixer for policy checks and reports.
	GetPolicyBackendOrFail(t testing.TB) DeployedPolicyBackend
}

// Deployed represents a deployed component
type Deployed interface {
}

// DeployedApp represents a deployed fake App within the mesh.
type DeployedApp interface {
	Deployed
	Name() string
	Endpoints() []DeployedAppEndpoint
	EndpointsForProtocol(protocol model.Protocol) []DeployedAppEndpoint
	Call(u *url.URL, count int, headers http.Header) (AppCallResult, error)
	CallOrFail(u *url.URL, count int, headers http.Header, t testing.TB) AppCallResult
}

// DeployedPolicyBackend represents a deployed fake policy backend for Mixer.
type DeployedPolicyBackend interface {
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

// DeployedAppEndpoint represents a single endpoint in a DeployedApp.
type DeployedAppEndpoint interface {
	Name() string
	Owner() DeployedApp
	Protocol() model.Protocol
	MakeURL() *url.URL
	MakeShortURL() *url.URL
}

// AppCallResult provides details about the result of a call
type AppCallResult struct {
	// Body is the body of the response
	Body string
	// CallIDs is a list of unique identifiers for individual requests made.
	CallIDs []string
	// Version is the version of the resource in the response
	Version []string
	// Port is the port of the resource in the response
	Port []string
	// Code is the response code
	ResponseCode []string
	// Host is the host returned by the response
	Host []string
}

// IsSuccess returns true if the request was successful
func (r *AppCallResult) IsSuccess() bool {
	return len(r.ResponseCode) > 0 && r.ResponseCode[0] == httpOK
}

// DeployedMixer represents a deployed Mixer instance.
type DeployedMixer interface {
	Deployed

	// Report is called directly with the given attributes.
	Report(t testing.TB, attributes map[string]interface{})
}

// DeployedPilot represents a deployed Pilot instance.
type DeployedPilot interface {
	Deployed
}

// DeployedFortioApp represents a deployed fake Fortio App within the mesh.
type DeployedFortioApp interface {
	Deployed
	CallFortio(arg string, path string) (FortioAppCallResult, error)
}

// FortioAppCallResult provides details about the result of a fortio call
type FortioAppCallResult struct {
	// The raw content of the response
	Raw string
}
