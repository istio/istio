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

	"istio.io/istio/pkg/test/impl/helm"
	"k8s.io/client-go/rest"
)

func GetClusterEnvironment(t testing.TB) *ClusterEnvironment {
	return &ClusterEnvironment{
		t: t,
	}
}

func GetLocalEnvironment(t testing.TB) *LocalEnvironment {
	return &LocalEnvironment{
		t: t,
	}
}

type Environment interface {
	GetApiServer() DeployedApiServer
	GetIstioComponent(k DeployedServiceKind) []DeployedIstioComponent
}

type Deployed interface {
}

type DeployedApiServer interface {
	Deployed
	Config() *rest.Config
}

type DeployedIstioComponent interface {
	Deployed
}

type DeployedServiceKind string

const (
	MixerComponent  = "mixer"
	PilotComponent  = "pilot"
	GalleyComponent = "galley"
)

type ClusterEnvironment struct {
	t testing.TB
}

var _ Environment = &ClusterEnvironment{}

type LocalEnvironment struct {
	t testing.TB
}

var _ Environment = &LocalEnvironment{}

func (e *ClusterEnvironment) Deploy(c *helm.Chart) {

}

func (e *ClusterEnvironment) GetApiServer() DeployedApiServer {
	return nil
}

func (e *ClusterEnvironment) GetIstioComponent(k DeployedServiceKind) []DeployedIstioComponent {
	return []DeployedIstioComponent{nil}
}

func (e *LocalEnvironment) StartApiServer() DeployedApiServer {
	return nil
}

func (e *LocalEnvironment) StartGalley() DeployedIstioComponent {
	return nil
}

func (e *LocalEnvironment) StartMixer() DeployedIstioComponent {
	return nil
}

func (e *LocalEnvironment) GetApiServer() DeployedApiServer {
	return nil
}

func (e *LocalEnvironment) GetIstioComponent(k DeployedServiceKind) []DeployedIstioComponent {
	return nil
}
