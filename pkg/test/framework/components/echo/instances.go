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
	"strings"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
)

// Instances contains the instances created by the builder with methods for filtering
type Instances []Instance

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

// IsDeployment returns true if there is only one deployment contained in the Instances
func (i Instances) IsDeployment() bool {
	return len(i.Services()) == 1
}

// Match filters instances that matcher the given Matcher
func (i Instances) Match(matches Matcher) Instances {
	out := make(Instances, 0)
	for _, i := range i {
		if matches(i) {
			out = append(out, i)
		}
	}
	return out
}

// Get finds the first Instance that matches the Matcher.
func (i Instances) Get(matches Matcher) (Instance, error) {
	res := i.Match(matches)
	if len(res) == 0 {
		return nil, errors.New("found 0 matching echo instances")
	}
	return res[0], nil
}

func (i Instances) GetOrFail(t test.Failer, matches Matcher) Instance {
	res, err := i.Get(matches)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (i Instances) Contains(instances ...Instance) bool {
	matches := i.Match(func(instance Instance) bool {
		for _, ii := range instances {
			if ii == instance {
				return true
			}
		}
		return false
	})
	return len(matches) > 0
}

func (i Instances) ContainsMatch(matches Matcher) bool {
	return len(i.Match(matches)) > 0
}

// Services is a set of Instances that share the same FQDN. While an Instance contains
// multiple deployments (a single service in a single cluster), Instances contains multiple
// deployments that may contain multiple Services.
type Services []Instances

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

// GetByService finds the first Instances with the given Service name. It is possible to have multiple deployments
// with the same service name but different namespaces (and therefore different FQDNs). Use caution when relying on
// Service.
func (d Services) GetByService(service string) Instances {
	for _, instances := range d {
		if instances[0].Config().Service == service {
			return instances
		}
	}
	return nil
}

// Services gives the service names of each deployment in order.
func (d Services) Services() []string {
	var out []string
	for _, instances := range d {
		out = append(out, instances[0].Config().Service)
	}
	return out
}

// FQDNs gives the fully-qualified-domain-names each deployment in order.
func (d Services) FQDNs() []string {
	var out []string
	for _, instances := range d {
		out = append(out, instances[0].Config().ClusterLocalFQDN())
	}
	return out
}

func (d Services) Instances() Instances {
	var out Instances
	for _, instances := range d {
		out = append(out, instances...)
	}
	return out
}

func (d Services) MatchFQDNs(fqdns ...string) Services {
	match := map[string]bool{}
	for _, fqdn := range fqdns {
		match[fqdn] = true
	}
	var out Services
	for _, instances := range d {
		if match[instances[0].Config().ClusterLocalFQDN()] {
			out = append(out, instances)
		}
	}
	return out
}

// Services must be sorted to make sure tests have consistent ordering
var _ sort.Interface = Services{}

// Len returns the number of deployments
func (d Services) Len() int {
	return len(d)
}

// Less returns true if the element at i should appear before the element at j in a sorted Services
func (d Services) Less(i, j int) bool {
	return strings.Compare(d[i][0].Config().ClusterLocalFQDN(), d[j][0].Config().ClusterLocalFQDN()) < 0
}

// Swap switches the positions of elements at i and j (used for sorting).
func (d Services) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}
