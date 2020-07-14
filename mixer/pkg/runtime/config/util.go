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

package config

import (
	"fmt"
	"strings"
)

// isFQN returns true if the name is fully qualified.
// every resource name is defined by Key.String()
// shortname.kind.namespace
func isFQN(name string) bool {
	c := 0
	for _, ch := range name {
		if ch == '.' {
			c++
		}

		if c > 2 {
			return false
		}
	}

	return c == 2
}

// canonicalize ensures that the name is fully qualified.
// NOTE: this multiple option return is temporary and will be removed after we delete / deprecated old style configs.
func canonicalize(name string, desiredKind, namespace string) (fqn string, alternate string) {
	if isFQN(name) {
		return name, ""
	}
	parts := strings.Split(name, ".")
	if len(parts) == 1 {
		return fmt.Sprintf("%s.%s.%s", name, desiredKind, namespace), ""

	}
	if len(parts) == 2 {
		// this case is ambiguous between old config model and new; we don't know if 2nd part
		// is the kind or the namespace. Let's return both the potential FQNs and let the caller match it against the
		// desired lookup.
		return fmt.Sprintf("%s.%s.%s", parts[0], parts[1], namespace),
			fmt.Sprintf("%s.%s.%s", parts[0], desiredKind, parts[1])
	}

	return name, ""
}

// ExtractShortName extracts the 'name' portion of the FQN.
func ExtractShortName(name string) string {
	if isFQN(name) {
		return name[0:strings.Index(name, ".")]
	}

	return name
}
