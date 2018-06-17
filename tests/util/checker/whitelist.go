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

// Whitelist determines if rules are whitelisted for the given paths.
type Whitelist struct {
	// Map from path to whitelisted rules.
	ruleWhitelist []string
}

// NewWhitelist creates and returns a Whitelist object.
func NewWhitelist(ruleWhitelist []string) *Whitelist {
	return &Whitelist{ruleWhitelist: ruleWhitelist}
}

// Apply returns true if the given rule is whitelisted.
func (wl *Whitelist) Apply(rule Rule) bool {
	for _, skipRule := range wl.ruleWhitelist {
		if skipRule == rule.GetID() || skipRule == "*" {
			return true
		}
	}
	return false
}
