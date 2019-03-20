// Copyright 2019 Istio Authors
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

package authplugins

import (
	"istio.io/istio/galley/pkg/authplugin"

	"istio.io/istio/galley/pkg/authplugins/google"
	"istio.io/istio/galley/pkg/authplugins/none"
)

// Inventory returns a slice of all supported plugins. For new plugins
// add them here.
func Inventory() []authplugin.InfoFn {
	return []authplugin.InfoFn{
		google.GetInfo,
		none.GetInfo,
	}
}

// AuthMap goes ahead and runs each GetInfo function and produces a
// map of plugin names to GetAuth functions.
func AuthMap() map[string]authplugin.AuthFn {
	m := make(map[string]authplugin.AuthFn)
	for _, g := range Inventory() {
		i := g()
		m[i.Name] = i.GetAuth
	}
	return m
}
