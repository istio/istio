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

package match

import (
	"path/filepath"
	"strings"

	"istio.io/pkg/log"
)

func MatchesMap(selection, cluster map[string]string) bool {
	if len(selection) == 0 {
		return true
	}
	if len(cluster) == 0 {
		return false
	}

	for ks, vs := range selection {
		vc, ok := cluster[ks]
		if !ok {
			return false
		}
		if !MatchesGlob(vc, vs) {
			return false
		}
	}
	return true
}

func MatchesGlobs(matchString string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	if len(patterns) == 1 {
		p := strings.TrimSpace(patterns[0])
		if p == "" || p == "*" {
			return true
		}
	}

	for _, p := range patterns {
		if MatchesGlob(matchString, p) {
			return true
		}
	}
	return false
}

func MatchesGlob(matchString, pattern string) bool {
	match, err := filepath.Match(pattern, matchString)
	if err != nil {
		// Shouldn't be here as prior validation is assumed.
		log.Errorf("Unexpected filepath error for %s match %s: %s", pattern, matchString, err)
		return false
	}
	return match
}
