// Copyright 2018 Istio Authors. All Rights Reserved.
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

package checker

import (
	"log"
	"path/filepath"
)

// Whitelist determines if rules are whitelisted for the given paths.
type Whitelist struct {
	// Map from path to whitelisted rules.
	ruleWhitelist map[string][]string
}

// NewWhitelist creates and returns a Whitelist object.
func NewWhitelist(ruleWhitelist map[string][]string) *Whitelist {
	return &Whitelist{ruleWhitelist: ruleWhitelist}
}

// Apply returns true if the given rule is whitelisted for the given path.
func (wl *Whitelist) Apply(path string, rule Rule) bool {
	for _, skipRule := range wl.getWhitelistedRules(path) {
		if skipRule == rule.GetID() {
			return true
		}
	}
	return false
}

// getWhitelistedRules returns the whitelisted rule given the path
func (wl *Whitelist) getWhitelistedRules(path string) []string {
	// Check whether path is whitelisted
	for wp, whitelistedRules := range wl.ruleWhitelist {
		// filepath.Match is needed for canonical matching
		matched, err := filepath.Match(wp, path)
		if err != nil {
			log.Printf("file match returns error: %v", err)
		}
		if matched {
			return whitelistedRules
		}
	}
	return []string{}
}
