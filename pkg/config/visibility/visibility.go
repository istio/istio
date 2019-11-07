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

package visibility

import "fmt"

// Instance defines whether a given config or service is exported to local namespace, all namespaces or none
type Instance string

const (
	// Private implies namespace local config
	Private Instance = "."
	// Public implies config is visible to all
	Public Instance = "*"
)

// Validate a visibility value.
func (v Instance) Validate() (errs error) {
	switch v {
	case Private, Public:
		return nil
	default:
		return fmt.Errorf("only . or * is allowed in the exportTo in the current release")
	}
}
