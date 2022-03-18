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

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
)

var _ Target = Instances{}

// Instances contains the instances created by the builder with methods for filtering
type Instances []Instance

func (i Instances) NamespacedName() model.NamespacedName {
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
	if len(i) == 0 {
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

// Append the given instances together at the end of this Instances and return a new Instances.
// Does not modify this Instances.
func (i Instances) Append(instances ...Instance) Instances {
	return append(append(Instances{}, i...), instances...)
}
