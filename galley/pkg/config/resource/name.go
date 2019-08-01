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

import "strings"

// Name of the resource. It is unique within a given set of resource of the same collection.
type Name struct{ string }

// NewName returns a Name from namespace and name.
func NewName(namespace, local string) Name {
	if namespace == "" {
		return Name{string: local}
	}

	return Name{string: namespace + "/" + local}
}

// String inteface implementation.
func (n Name) String() string {
	return n.string
}

// InterpretAsNamespaceAndName tries to split the name as namespace and name
func (n Name) InterpretAsNamespaceAndName() (string, string) {
	parts := strings.SplitN(n.string, "/", 2)
	if len(parts) == 1 {
		return "", parts[0]
	}

	return parts[0], parts[1]
}
