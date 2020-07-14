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

package dispatcher

import (
	"sync/atomic"

	"istio.io/istio/mixer/pkg/runtime/routing"
)

// RoutingContext is the currently active dispatching context, based on a config snapshot. As config changes,
// the current/live RoutingContext also changes.
type RoutingContext struct {
	// the routing table of this context.
	Routes *routing.Table

	// the current reference count. Indicates how many calls are currently using this RoutingContext.
	refCount int32
}

// incRef increases the reference count on the RoutingContext.
func (rc *RoutingContext) incRef() {
	atomic.AddInt32(&rc.refCount, 1)
}

// decRef decreases the reference count on the RoutingContext.
func (rc *RoutingContext) decRef() {
	atomic.AddInt32(&rc.refCount, -1)
}

// GetRefs returns the current reference count on the dispatch context.
func (rc *RoutingContext) GetRefs() int32 {
	return atomic.LoadInt32(&rc.refCount)
}
