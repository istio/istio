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

package api

import (
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/ids"
)

// Environment internal interface for all environments.
type Environment interface {
	Component

	// TODO(nmittler): Remove this.
	// Evaluate the given template with environment specific template variables.
	Evaluate(tmpl string) (string, error)

	DumpState(context string)
}

// GetEnvironment from the repository
func GetEnvironment(r component.Repository) Environment {
	e := r.GetComponent(ids.Environment)
	if e == nil {
		return nil
	}
	return e.(Environment)
}
