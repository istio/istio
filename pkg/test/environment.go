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
	"testing"

	"k8s.io/client-go/rest"
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
	GetApp(name string) DeployedApp
}

// Deployed represents a deployed component
type Deployed interface {
}

// DeployedApp represents a deployed fake App within the mesh.
type DeployedApp interface {
	Deployed
	Call(target DeployedApp) AppRequestInfo
	Expect(info AppRequestInfo) error
}

// AppRequestInfo contains metadata about a request that was performed through a fake App.
type AppRequestInfo struct {
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
