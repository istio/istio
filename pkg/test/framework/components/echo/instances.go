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
	"istio.io/istio/pkg/test/framework/resource"
)

// Instances contains the instances created by the builder with methods for filtering
type Instances []Instance

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

// InCluster matches instances deployed on the given cluster.
func InCluster(c resource.Cluster) Matcher {
	return func(i Instance) bool {
		return c.Index() == i.Config().Cluster.Index()
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