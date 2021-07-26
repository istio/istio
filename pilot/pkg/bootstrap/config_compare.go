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

package bootstrap

import (
	"reflect"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
)

// EqualsFunc defines a comparison func for an API proto.
type EqualsFunc func(prev config.Config, curr config.Config) bool

var equalsFuncs = make(map[string]EqualsFunc)

func registerEqualsFunc(name string, equals EqualsFunc) {
	equalsFuncs[name] = equals
}

// destinationRuleEquals checks proxy policies
var destinationRuleEquals = func(prev config.Config, curr config.Config) bool {
	prevspec := prev.Spec
	currspec := curr.Spec
	// TODO(ramaraochavali): optimize this by comparing relevant fields only.
	return reflect.DeepEqual(prevspec, currspec)
}

var virtualServiceEquals = func(prev config.Config, curr config.Config) bool {
	prevspec := prev.Spec
	currspec := curr.Spec
	// TODO(ramaraochavali): optimize this by comparing relevant fields only.
	return reflect.DeepEqual(prevspec, currspec)
}

func init() {
	// TODO(ramaraochavali): add functions for other configs.
	registerEqualsFunc(gvk.DestinationRule.String(), destinationRuleEquals)
	registerEqualsFunc(gvk.VirtualService.String(), virtualServiceEquals)
}

// needsPush checks whether the passed in config are same from config generation perspective
// and a push needs to be triggered. This is to avoid unnecessary pushes only when labels
// have changed for example.
func needsPush(prev config.Config, curr config.Config) bool {
	if prev.GroupVersionKind != curr.GroupVersionKind {
		// This should never happen.
		return false
	}
	if ef, exists := equalsFuncs[prev.GroupVersionKind.String()]; exists {
		// We should trigger push if configs are not equal.
		return !ef(prev, curr)
	}
	return true
}
