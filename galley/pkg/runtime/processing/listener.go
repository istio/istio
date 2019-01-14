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

package processing

import "istio.io/istio/galley/pkg/runtime/resource"

// Listener gets notified when resource of a given type has changed.
type Listener interface {
	TypeChanged(t resource.TypeURL)
}

// ListenerFromFn creates a listener based on the given function
func ListenerFromFn(fn func(t resource.TypeURL)) Listener {
	return &fnListener{
		fn: fn,
	}
}

var _ Listener = &fnListener{}

type fnListener struct {
	fn func(t resource.TypeURL)
}

func (h *fnListener) TypeChanged(t resource.TypeURL) {
	h.fn(t)
}
