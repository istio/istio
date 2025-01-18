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

package echo

import (
	"errors"
	"sort"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
)

var _ Target = Instances{}

// Instances contains the instances created by the builder with methods for filtering
type Instances []Instance

func (i Instances) ServiceName() string {
	return i.Config().Service
}

func (i Instances) NamespaceName() string {
	return i.Config().NamespaceName()
}

func (i Instances) SpiffeIdentity() string {
	return i.Config().SpiffeIdentity()
}

func (i Instances) ClusterLocalFQDN() string {
	return i.Config().ClusterLocalFQDN()
}

func (i Instances) ClusterSetLocalFQDN() string {
	return i.Config().ClusterSetLocalFQDN()
}

func (i Instances) NamespacedName() NamespacedName {
	return i.Config().NamespacedName()
}

func (i Instances) PortForName(name string) Port {
	return i.Config().Ports.MustForName(name)
}

func (i Instances) Config() Config {
	return i.mustGetFirst().Config()
}

func (i Instances) Instances() Instances {
	return i
}

func (i Instances) mustGetFirst() Instance {
	if i.Len() == 0 {
		panic("instances are empty")
	}
	return i[0]
}

// Callers is a convenience method to convert Instances into Callers.
func (i Instances) Callers() Callers {
	var out Callers
	for _, instance := range i {
		out = append(out, instance)
	}
	return out
}

// Clusters returns a list of cluster names that the instances are deployed in
func (i Instances) Clusters() cluster.Clusters {
	clusters := map[string]cluster.Cluster{}
	for _, instance := range i {
		clusters[instance.Config().Cluster.Name()] = instance.Config().Cluster
	}
	out := make(cluster.Clusters, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, c)
	}
	return out
}

// ForCluster returns a list of instances that match the cluster name
func (i Instances) ForCluster(name string) Instances {
	out := make(Instances, 0, len(i))
	for _, c := range i {
		if c.Config().Cluster.Name() == name {
			out = append(out, c)
		}
	}
	return out
}

func (i Instances) Workloads() (Workloads, error) {
	var out Workloads
	for _, inst := range i {
		ws, err := inst.Workloads()
		if err != nil {
			return nil, err
		}
		out = append(out, ws...)
	}

	if len(out) == 0 {
		return nil, errors.New("got 0 workloads")
	}

	return out, nil
}

func (i Instances) WorkloadsOrFail(t test.Failer) Workloads {
	t.Helper()
	out, err := i.Workloads()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (i Instances) MustWorkloads() Workloads {
	out, err := i.Workloads()
	if err != nil {
		panic(err)
	}
	return out
}

// IsDeployment returns true if there is only one deployment contained in the Instances
func (i Instances) IsDeployment() bool {
	return len(i.Services()) == 1
}

func (i Instances) ContainsTarget(t Target) bool {
	return i.Contains(t.Instances()...)
}

func (i Instances) Contains(instances ...Instance) bool {
	for _, thatI := range instances {
		found := false
		for _, thisI := range i {
			if thisI == thatI {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Services groups the Instances by FQDN. Each returned element is an Instances
// containing only instances of a single service.
func (i Instances) Services() Services {
	grouped := map[string]Instances{}
	for _, instance := range i {
		k := instance.Config().ClusterLocalFQDN()
		grouped[k] = append(grouped[k], instance)
	}
	var out Services
	for _, deployment := range grouped {
		out = append(out, deployment)
	}
	sort.Stable(out)
	return out
}

// Copy this Instances array.
func (i Instances) Copy() Instances {
	return append(Instances{}, i...)
}

// Append returns a new Instances array with the given values appended.
func (i Instances) Append(instances Instances) Instances {
	return append(i.Copy(), instances...)
}

// Restart each Instance
func (i Instances) Restart() error {
	g := multierror.Group{}
	for _, app := range i {
		g.Go(app.Restart)
	}
	return g.Wait().ErrorOrNil()
}

func (i Instances) Len() int {
	return len(i)
}

func (i Instances) NamespacedNames() NamespacedNames {
	out := make(NamespacedNames, 0, i.Len())
	for _, ii := range i {
		out = append(out, ii.NamespacedName())
	}

	sort.Stable(out)
	return out
}
