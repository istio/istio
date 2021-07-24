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

// CompareFunc defines a comparison func for an API proto.
type CompareFunc func(prev config.Config, curr config.Config) bool

var compareFuncs = make(map[string]CompareFunc)

func registerCompareFunc(name string, compare CompareFunc) {
	compareFuncs[name] = compare
}

// compareDestinationRule checks proxy policies
var compareDestinationRule = func(prev config.Config, curr config.Config) bool {
	prevspec := prev.Spec
	currspec := curr.Spec
	// TODO(ramaraochavali): optimize this by comparing relevant fields only.
	return reflect.DeepEqual(prevspec, currspec)
}

var compareVirtuaService = func(prev config.Config, curr config.Config) bool {
	prevspec := prev.Spec
	currspec := curr.Spec
	// TODO(ramaraochavali): optimize this by comparing relevant fields only.
	return reflect.DeepEqual(prevspec, currspec)
}

func init() {
	// TODO(ramaraochavali): add functions for other configs.
	registerCompareFunc(gvk.DestinationRule.String(), compareDestinationRule)
	registerCompareFunc(gvk.VirtualService.String(), compareVirtuaService)
}

func equals(prev config.Config, curr config.Config) bool {
	if prev.GroupVersionKind != curr.GroupVersionKind {
		return true
	}
	if cf, exists := compareFuncs[prev.GroupVersionKind.String()]; exists {
		return cf(prev, curr)
	}
	return true
}
