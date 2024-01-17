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

package xds

import (
	"fmt"
	"strings"
)

func atMostNJoin(data []string, limit int) string {
	if limit == 0 || limit == 1 {
		// Assume limit >1, but make sure we dpn't crash if someone does pass those
		return strings.Join(data, ", ")
	}
	if len(data) == 0 {
		return ""
	}
	if len(data) < limit {
		return strings.Join(data, ", ")
	}
	return strings.Join(data[:limit-1], ", ") + fmt.Sprintf(", and %d others", len(data)-limit+1)
}
