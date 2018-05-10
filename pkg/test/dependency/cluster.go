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

package dependency

import (
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/test/environment"
	"istio.io/istio/pkg/test/internal"
)

// GKE dependency
var GKE Dependency = &clusterDependency{gke: true}

// ClusterDependency represents a typed ClusterDependency dependency.
type clusterDependency struct {
	gke bool
}

// Dependency is the default dependency
var _ Dependency = &clusterDependency{}
var _ internal.Stateful = &clusterDependency{}

func (a *clusterDependency) String() string {
	return "cluster"
}

func (a *clusterDependency) Initialize(env environment.Interface) (interface{}, error) {
	// TODO
	return nil, nil
}

func (a *clusterDependency) Reset(env environment.Interface, state interface{}) error {
	return nil
}

func (a *clusterDependency) Cleanup(env environment.Interface, state interface{}) {
}

// GetConfig returns the configuration
func (a *clusterDependency) GetConfig() *rest.Config {
	return nil
}
