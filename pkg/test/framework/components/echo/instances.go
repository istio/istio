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
	"strings"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
)

// Instances contains the instances created by the builder with methods for filtering
type Instances []Instance

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

// Matcher is used to filter matching instances
type Matcher func(Instance) bool

// And combines two or more matches. Example:
//     Service("a").And(InCluster(c)).And(Match(func(...))
func (m Matcher) And(other Matcher) Matcher {
	return func(i Instance) bool {
		return m(i) && other(i)
	}
}

// ServicePrefix matches instances whose service name starts with the given prefix.
func ServicePrefix(prefix string) Matcher {
	return func(i Instance) bool {
		return strings.HasPrefix(i.Config().Service, prefix)
	}
}

// Service matches instances with have the given service name.
func Service(value string) Matcher {
	return func(i Instance) bool {
		return value == i.Config().Service
	}
}

// Service matches instances within the given namespace name.
func Namespace(namespace string) Matcher {
	return func(i Instance) bool {
		return i.Config().Namespace.Name() == namespace
	}
}

// InCluster matches instances deployed on the given cluster.
func InCluster(c cluster.Cluster) Matcher {
	return func(i Instance) bool {
		return c.Name() == i.Config().Cluster.Name()
	}
}

// InNetwork matches instances deployed in the given network.
func InNetwork(n string) Matcher {
	return func(i Instance) bool {
		return i.Config().Cluster.NetworkName() == n
	}
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
