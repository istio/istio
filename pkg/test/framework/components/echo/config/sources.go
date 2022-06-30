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

package config

import (
	"istio.io/istio/pkg/test/framework/components/echo/config/param"
	"istio.io/istio/pkg/test/framework/components/namespace"
)

// Sources provides common operations that can be performed on a list of Source objects.
type Sources []Source

// WithParams creates a new Sources with the given template parameters.
// See Source.WithParams.
func (s Sources) WithParams(params param.Params) Sources {
	out := make(Sources, 0, len(s))
	for _, src := range s {
		out = append(out, src.WithParams(params))
	}
	return out
}

// WithNamespace creates a new Sources with the given namespace parameter set.
// See Source.WithNamespace.
func (s Sources) WithNamespace(ns namespace.Instance) Sources {
	out := make(Sources, 0, len(s))
	for _, src := range s {
		out = append(out, src.WithNamespace(ns))
	}
	return out
}
