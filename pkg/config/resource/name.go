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
	"strings"
)

// Namespace containing the resource.
type Namespace string

func (n Namespace) String() string {
	return string(n)
}

// LocalName that uniquely identifies the resource within the Namespace.
type LocalName string

func (n LocalName) String() string {
	return string(n)
}

// FullName is a name that uniquely identifies a resource within the mesh.
type FullName struct {
	Namespace Namespace
	Name      LocalName
}

// String interface implementation.
func (n FullName) String() string {
	if len(n.Namespace) == 0 {
		return string(n.Name)
	}
	return string(n.Namespace) + "/" + string(n.Name)
}

// NewShortOrFullName tries to parse the given name to resource.Name. If the name does not include namespace information,
// the defaultNamespace is used.
func NewShortOrFullName(defaultNamespace Namespace, name string) FullName {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 1 {
		return FullName{
			Namespace: defaultNamespace,
			Name:      LocalName(parts[0]),
		}
	}

	return FullName{
		Namespace: Namespace(parts[0]),
		Name:      LocalName(parts[1]),
	}
}

// Validate that the Name and Namespace are set.
func (n FullName) Validate() error {
	if len(n.Name) == 0 {
		return fmt.Errorf("invalid name '%s': name must not be empty", n.String())
	}
	return nil
}

// NewFullName creates a new FullName from the given Namespace and Name.
func NewFullName(ns Namespace, n LocalName) FullName {
	return FullName{
		Namespace: ns,
		Name:      n,
	}
}

// ParseFullName parses the given name string that was serialized via FullName.String()
func ParseFullName(name string) (FullName, error) {
	return ParseFullNameWithDefaultNamespace("", name)
}

// ParseFullName parses the given name string using defaultNamespace if no namespace is found.
func ParseFullNameWithDefaultNamespace(defaultNamespace Namespace, name string) (FullName, error) {
	out := NewShortOrFullName(defaultNamespace, name)

	if err := out.Validate(); err != nil {
		return FullName{}, fmt.Errorf("failed parsing name '%v': %v", name, err)
	}
	return out, nil
}
