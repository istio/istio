// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"fmt"
	"regexp"
	"strings"

	"istio.io/istio/operator/pkg/manifest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/yml"
)

func FilterManifest(t test.Failer, ms string, selectResources string) string {
	sm := getObjPathMap(selectResources)
	parsed, err := manifest.ParseMultiple(ms)
	if err != nil {
		t.Fatal(err)
	}
	parsed = slices.FilterInPlace(parsed, func(manifest manifest.Manifest) bool {
		for selected := range sm {
			re, err := buildResourceRegexp(strings.TrimSpace(selected))
			if err != nil {
				t.Fatal(err)
			}
			if re.MatchString(manifest.Hash()) {
				return true
			}
		}
		return false
	})

	return yml.JoinString(slices.Map(parsed, func(e manifest.Manifest) string {
		return e.Content
	})...) + "\n"
}

// buildResourceRegexp translates the resource indicator to regexp.
func buildResourceRegexp(s string) (*regexp.Regexp, error) {
	hash := strings.Split(s, ":")
	for i, v := range hash {
		if v == "" || v == "*" {
			hash[i] = ".*"
		}
	}
	return regexp.Compile(strings.Join(hash, ":"))
}

func getObjPathMap(rs string) map[string]string {
	rm := make(map[string]string)
	if len(rs) == 0 {
		return rm
	}
	for _, r := range strings.Split(rs, ",") {
		split := strings.Split(r, ":")
		if len(split) < 4 {
			rm[r] = ""
			continue
		}
		kind, namespace, name, path := split[0], split[1], split[2], split[3]
		obj := fmt.Sprintf("%v:%v:%v", kind, namespace, name)
		rm[obj] = path
	}
	return rm
}
