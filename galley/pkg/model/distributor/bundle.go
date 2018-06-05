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

package distributor

import (
	"fmt"

	"istio.io/istio/galley/pkg/model/component"
	"istio.io/istio/galley/pkg/model/resource"
)

// Bundle represents a self-contained bundle of configuration, from which distributable configuration
// can be obtained from.
//
// The contents of a bundle can be considered immutable, for config distribution purposes: Its method
// calls will always yield the same output.
type Bundle interface {
	fmt.Stringer

	// The Destination component that this config bundle is intended for.
	Destination() component.InstanceID

	// List the set of resources for distribution purposes.
	// TODO: Expand the method signature to accommodate various needs (i.e. version-leveling etc.)
	List(kind resource.Kind) []*resource.Entry
}
