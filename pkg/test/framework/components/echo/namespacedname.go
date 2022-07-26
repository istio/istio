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
	"sort"
	"strings"

	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/util/sets"
)

// NamespacedName represents the full name of a service.
type NamespacedName struct {
	// Namespace of the echo Instance. If not provided, a default namespace "apps" is used.
	Namespace namespace.Instance

	// Name of the service within the Namespace.
	Name string
}

// NamespaceName returns the string name of the namespace, or "" if Namespace is nil.
func (n NamespacedName) NamespaceName() string {
	if n.Namespace != nil {
		return n.Namespace.Name()
	}
	return ""
}

// String returns the Istio-formatted service name in the form of <namespace>/<name>.
func (n NamespacedName) String() string {
	return n.NamespaceName() + "/" + n.Name
}

// PrefixString returns a string in the form of <name>.<prefix>. This is helpful for
// providing more stable test names.
func (n NamespacedName) PrefixString() string {
	if n.Namespace == nil {
		return n.Name
	}
	return n.Name + "." + n.Namespace.Prefix()
}

var _ sort.Interface = NamespacedNames{}

// NamespacedNames is a list of NamespacedName.
type NamespacedNames []NamespacedName

func (n NamespacedNames) Less(i, j int) bool {
	return strings.Compare(n[i].PrefixString(), n[j].PrefixString()) < 0
}

func (n NamespacedNames) Swap(i, j int) {
	n[i], n[j] = n[j], n[i]
}

func (n NamespacedNames) Len() int {
	return len(n)
}

// Names returns the list of service names without any namespace appended.
func (n NamespacedNames) Names() []string {
	return n.uniqueSortedNames(func(nn NamespacedName) string {
		return nn.Name
	})
}

func (n NamespacedNames) NamesWithNamespacePrefix() []string {
	return n.uniqueSortedNames(func(nn NamespacedName) string {
		if nn.Namespace == nil {
			return nn.Name
		}
		return nn.Name + "." + nn.Namespace.Prefix()
	})
}

func (n NamespacedNames) uniqueSortedNames(getName func(NamespacedName) string) []string {
	set := sets.NewWithLength(n.Len())
	out := make([]string, 0, n.Len())
	for _, nn := range n {
		name := getName(nn)
		if !set.Contains(name) {
			set.Insert(name)
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
