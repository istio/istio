//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package server

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"istio.io/istio/galley/pkg/crd"
)

var mappingData = map[schema.GroupVersion]schema.GroupVersion{
	{
		Group:   "config.istio.io",
		Version: "v1alpha2",
	}: {
		Group:   "internal.istio.io",
		Version: "v1alpha2",
	},
}

// Mapping returns a mapping of v1 internal and public CRDs.
func Mapping() crd.Mapping {
	m, err := crd.NewMapping(mappingData)
	if err != nil {
		panic(err)
	}
	return m
}
