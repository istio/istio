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

package test

import (
	"net/http"
	"testing"

	"k8s.io/client-go/rest"

	"istio.io/istio/pilot/pkg/model"
)

const (
	httpOK = "200"
)

// Environment is a common interface for all testing environments
type Environment interface {
	Configure(config string)
	GetMixer() DeployedMixer
	GetPilot() DeployedPilot

	// GetAPIServer returns the deployed k8s API server
	GetAPIServer() DeployedAPIServer
	// GetIstioComponent gets the deployed configuration for all Istio components of the given kind.
	GetIstioComponent(k DeployedServiceKind) []DeployedIstioComponent
	// GetApp returns an app object for the given name.
	GetApp(name string) (DeployedApp, error)
	// GetAppOrFail attempts to return the app object for the given name, or fails the test if unsuccessful.
	GetAppOrFail(name string, t *testing.T) DeployedApp
}

// Deployed represents a deployed component
type Deployed interface {
}

// DeployedApp represents a deployed fake App within the mesh.
type DeployedApp interface {
	Deployed
	Endpoints() []DeployedAppEndpoint
	EndpointsForProtocol(protocol model.Protocol) []DeployedAppEndpoint
	Call(url string, count int, headers http.Header) (AppCallResult, error)
}

// DeployedAppEndpoint represents a single endpoint in a DeployedApp.
type DeployedAppEndpoint interface {
	Name() string
	Owner() DeployedApp
	Protocol() model.Protocol
	MakeURL(useFullDomain bool) string
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
	GetSpyAdapter() SpyAdapter
	Report(attributes map[string]interface{}) error
	Expect(str string) error
}

// DeployedPilot represents a deployed Pilot instance.
type DeployedPilot interface {
	Deployed
}

// SpyAdapter represents a remote Spy Adapter for Mixer.
type SpyAdapter interface {
	Expect(i []interface{}) bool
}

// DeployedAPIServer the configuration for a deployed k8s server
type DeployedAPIServer interface {
	Deployed
	Config() *rest.Config
}

// DeployedIstioComponent the configuration for a deployed Istio component
type DeployedIstioComponent interface {
	Deployed
}

// DeployedServiceKind an enum for the various types of deployed services
type DeployedServiceKind string

const (
	//MixerComponent  = "mixer"
	//PilotComponent  = "pilot"

	// GalleyComponent enum value for Galley.
	GalleyComponent = "galley"
)

// GetEnvironment returns the current, ambient environment.
func GetEnvironment(t *testing.T) Environment {
	return nil
}
