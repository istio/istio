/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package manifest

import (
	"regexp"
	"strings"

	"k8s.io/helm/pkg/releaseutil"
)

var (
	kindRegex = regexp.MustCompile("kind:(.*)\n")
)

// SplitManifests takes a map of rendered templates and splits them into the
// detected manifests.
func SplitManifests(templates map[string]string) []Manifest {
	var listManifests []Manifest
	// extract kind and name
	for k, v := range templates {
		match := kindRegex.FindStringSubmatch(v)
		h := "Unknown"
		if len(match) == 2 {
			h = strings.TrimSpace(match[1])
		}
		m := Manifest{Name: k, Content: v, Head: &releaseutil.SimpleHead{Kind: h}}
		listManifests = append(listManifests, m)
	}

	return listManifests
}
