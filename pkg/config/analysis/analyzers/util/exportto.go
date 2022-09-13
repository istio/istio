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

package util

// IsExportToAllNamespaces returns true if export to applies to all namespaces
// and false if it is set to namespace local.
func IsExportToAllNamespaces(exportTos []string) bool {
	exportedToAll := false
	for _, e := range exportTos {
		if e == ExportToAllNamespaces {
			// give preference to "*" and stop iterating
			exportedToAll = true
			break
		}
	}
	if len(exportTos) == 0 {
		exportedToAll = true
	}
	return exportedToAll
}
