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

import "istio.io/istio/pkg/test/framework/components/cluster"

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

	EnvironmentName() string

	// Clusters in this Environment. There will always be at least one.
	Clusters() cluster.Clusters

	// AllClusters in this Environment, including external control planes.
	AllClusters() cluster.Clusters
	IsMultiCluster() bool
	IsMultiNetwork() bool
}
