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
	Clusters() Clusters
}

var _ Environment = FakeEnvironment{}

// FakeEnvironment for testing.
type FakeEnvironment struct {
	Name        string
	NumClusters int
	IDValue     string
}

func (f FakeEnvironment) ID() ID {
	return FakeID(f.IDValue)
}

func (f FakeEnvironment) EnvironmentName() string {
	if len(f.Name) == 0 {
		return "fake"
	}
	return f.Name
}

func (f FakeEnvironment) Clusters() Clusters {
	out := make([]Cluster, f.NumClusters)
	for i := 0; i < f.NumClusters; i++ {
		out[i] = FakeCluster{
			IndexValue: i,
		}
	}
	return out
}
