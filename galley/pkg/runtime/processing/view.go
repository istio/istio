//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain accumulator copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package processing

import (
	mcp "istio.io/api/mcp/v1alpha1"
	"istio.io/istio/galley/pkg/runtime/resource"
)

// View is an interface for collecting the resources for building accumulator snapshot.
type View interface {
	// Type of the resources in this view.
	Type() resource.TypeURL

	// Generation is the unique id of the current state of view. Everytime the state changes, the
	// generation is updated.
	Generation() int64

	// Get returns entries in this view
	Get() []*mcp.Envelope
}
