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

package krt

import "istio.io/istio/pkg/ptr"

// RecomputeTrigger trigger provides an escape hatch to allow krt transformations to depend on external state and recompute
// correctly when those change.
// Typically, all state is registered and fetched through krt.Fetch. Through this mechanism, any changes are automatically
// propagated through the system to dependencies.
// In some cases, it may not be feasible to get all state into krt; hopefully, this is a temporary state.
// RecomputeTrigger works around this by allowing an explicit call to recompute a collection; the caller must be sure to call Trigger()
// any time the state changes.
type RecomputeTrigger struct {
	inner StaticSingleton[int]
	i     int
}

func NewRecomputeTrigger() *RecomputeTrigger {
	inner := NewStatic[int](ptr.Of(0))
	return &RecomputeTrigger{inner: inner, i: 0}
}

// TriggerRecomputation tells all dependants to recompute
func (r *RecomputeTrigger) TriggerRecomputation() {
	r.i++
	r.inner.Set(ptr.Of(r.i))
}

// MarkDependant marks the given context as depending on this trigger. This registers it to be recomputed when TriggerRecomputation
// is called.
func (r *RecomputeTrigger) MarkDependant(ctx HandlerContext) {
	_ = Fetch(ctx, r.inner.AsCollection())
}
