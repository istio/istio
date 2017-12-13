// Copyright 2017 Istio Authors
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

package config

import (
	"istio.io/istio/mixer/pkg/config/proto"
	"istio.io/istio/mixer/pkg/expr"
)

var emptyFinder = &attributeFinder{attrs: make(map[string]*istio_mixer_v1_config.AttributeManifest_AttributeInfo, 0)}

// attributeFinder exposes expr.AttributeDescriptorFinder
type attributeFinder struct {
	attrs map[string]*istio_mixer_v1_config.AttributeManifest_AttributeInfo
}

var _ expr.AttributeDescriptorFinder = &attributeFinder{}

// GetAttribute finds an attribute by name.
// This function is only called when a new handler is instantiated.
func (a attributeFinder) GetAttribute(name string) *istio_mixer_v1_config.AttributeManifest_AttributeInfo {
	return a.attrs[name]
}
