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

package resource

import (
	"fmt"

	"istio.io/istio/pkg/test/framework/resource/environment"
)

// EnvironmentFactory creates an Environment.
type EnvironmentFactory func(ctx Context) (Environment, error)

var _ EnvironmentFactory = NilEnvironmentFactory

// NilEnvironmentFactory is an EnvironmentFactory that returns nil.
func NilEnvironmentFactory(Context) (Environment, error) {
	return nil, nil
}

// Environment is the ambient environment that the test runs in.
type Environment interface {
	Resource

	EnvironmentName() environment.Name

	// IsMulticluster is a utility method that indicates whether there are multiple Clusters available.
	IsMulticluster() bool

	// IsMultinetwork returns true if there are multiple networks in the cluster topology
	IsMultinetwork() bool

	// Clusters in this Environment. There will always be at least one.
	Clusters() []Cluster

	ControlPlaneClusters(excludedClusters ...Cluster) []Cluster

	// GetControlPlaneCluster returns the cluster running the control plane for the given cluster based on the ControlPlaneTopology.
	GetControlPlaneCluster(Cluster) (Cluster, error)

	// Case calls the given function if this environment has the given name.
	Case(e environment.Name, fn func())
}

// UnsupportedEnvironment generates an error indicating that the given environment is not supported.
func UnsupportedEnvironment(env Environment) error {
	return fmt.Errorf("unsupported environment: %q", string(env.EnvironmentName()))
}

var _ Environment = FakeEnvironment{}

// FakeEnvironment for testing.
type FakeEnvironment struct {
	Name        string
	NumClusters int
	IDValue     string
}

func (f FakeEnvironment) IsMulticluster() bool {
	return f.NumClusters > 1
}

func (f FakeEnvironment) IsMultinetwork() bool {
	return false
}

func (f FakeEnvironment) ID() ID {
	return FakeID(f.IDValue)
}

func (f FakeEnvironment) EnvironmentName() environment.Name {
	if len(f.Name) == 0 {
		return "fake"
	}
	return environment.Name(f.Name)
}

func (f FakeEnvironment) Clusters() []Cluster {
	out := make([]Cluster, f.NumClusters)
	for i := 0; i < f.NumClusters; i++ {
		out[i] = FakeCluster{
			IndexValue: i,
		}
	}
	return out
}

func (f FakeEnvironment) ControlPlaneClusters(excludedClusters ...Cluster) []Cluster {
	return f.Clusters()
}

func (f FakeEnvironment) GetControlPlaneCluster(cluster Cluster) (Cluster, error) {
	return cluster, nil
}

func (f FakeEnvironment) Case(environment.Name, func()) {
	panic("not implemented")
}
