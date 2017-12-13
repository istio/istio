// Copyright 2017 Istio Authors
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
	"strings"
)

// isFQN returns true if the name is fully qualified.
// every resource name is defined by Key.String()
// shortname.kind.namespace
func isFQN(name string) bool {
	// TODO: This should be made no-alloc
	return len(strings.Split(name, ".")) == 3
}

// canonicalize ensures that the name is fully qualified.
func canonicalize(name string, namespace string) string {
	if isFQN(name) {
		return name
	}

	return name + "." + namespace
}
