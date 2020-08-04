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

package status

import (
	"istio.io/istio/galley/pkg/config/analysis/diag"
)

// DocRef is the doc ref value used by the status controller
const DocRef = "status-controller"

// toStatusValue converts a set of diag.Messages to a status value.
func toStatusValue(msgs diag.Messages) interface{} {
	if len(msgs) == 0 {
		return nil
	}

	result := make([]interface{}, 0)
	for _, m := range msgs {
		m.DocRef = DocRef
		// For the purposes of status update, the origin field is redundant
		// since we're attaching the message to the origin resource.
		result = append(result, m.Unstructured(false))
	}

	return result
}
