// Copyright 2019 Istio Authors
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
	"errors"
	"fmt"
	"strings"
)

// Name of the resource. It is unique within a given set of resource of the same collection.
type Name struct{ string }

// NewName returns a Name from namespace and name.
func NewName(namespace, local string) Name {
	if namespace == "" {
		return Name{string: local}
	}

	return Name{string: namespace + "/" + local}
}

// NewFullName returns a given name as a resource Name, validating it for correctness
func NewFullName(name string) (Name, error) {
	if name == "" {
		return Name{string: ""}, errors.New("invalid name: can not be empty")
	}

	ns, n := splitNamespaceAndName(name)

	if ns == "" {
		return Name{string: ""}, fmt.Errorf("invalid name %s: namespace must not be empty", name)
	}
	if n == "" {
		return Name{string: ""}, fmt.Errorf("invalid name %s: name must not be empty", name)
	}

	return Name{string: name}, nil
}

// NewShortOrFullName tries to parse the given name to resource.Name. If the name does not include namespace information,
// the defaultNamespace is used.
func NewShortOrFullName(defaultNamespace, name string) Name {
	ns, host := splitNamespaceAndName(name)
	if ns == "" {
		return NewName(defaultNamespace, host)
	}
	return NewName(ns, host)
}

// String interface implementation.
func (n Name) String() string {
	return n.string
}

// splitNamespaceAndName tries to split the string as namespace and name
func splitNamespaceAndName(name string) (string, string) {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}

	return parts[0], parts[1]
}

// InterpretAsNamespaceAndName tries to split the name as namespace and name
func (n Name) InterpretAsNamespaceAndName() (string, string) {
	return splitNamespaceAndName(n.String())
}
