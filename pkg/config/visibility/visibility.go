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

package visibility

import (
	"fmt"

	"istio.io/istio/pkg/config/labels"
)

// Instance defines whether a given config or service is exported to local namespace, some set of namespaces, or
// all namespaces or none
type Instance string

const (
	// Private implies namespace local config
	Private Instance = "."
	// Public implies config is visible to all
	Public Instance = "*"
	// None implies service is visible to no one. Used for services only
	None Instance = "~"
)

// Validate a visibility value ( ./*/~/some namespace name which is DNS1123 label)
func (v Instance) Validate() (errs error) {
	switch v {
	case Private, Public:
		return nil
	case None:
		return fmt.Errorf("exportTo ~ (none) is not allowed for Istio configuration objects")
	default:
		if !labels.IsDNS1123Label(string(v)) {
			return fmt.Errorf("only .,*,~, or a valid DNS 1123 label is allowed as exportTo entry")
		}
	}
	return nil
}
