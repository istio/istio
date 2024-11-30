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

package controller

import (
	v1 "k8s.io/api/core/v1"

	"istio.io/api/annotation"
)

// serviceFilter is used to check whether to filter out unrelated services
// true indicates this event is filtered out, stop processing
func serviceFilter(old, cur *v1.Service) bool {
	// service add or delete event
	if old == nil {
		if cur.Annotations[annotation.NetworkingExportTo.Name] == "~" {
			return true
		}

		return false
	}

	// service update, but both with exportTo = `~`
	if old.Annotations[annotation.NetworkingExportTo.Name] == cur.Annotations[annotation.NetworkingExportTo.Name] &&
		cur.Annotations[annotation.NetworkingExportTo.Name] == "~" {
		return true
	}

	return false
}
