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

package krt_test

import (
	"testing"

	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
)

func TestRecomputeTrigger(t *testing.T) {
	rt := krt.NewRecomputeTrigger()
	col1 := krt.NewStatic(ptr.Of("foo")).AsCollection()
	response := "foo"
	col2 := krt.NewCollection(col1, func(ctx krt.HandlerContext, i string) *string {
		rt.MarkDependant(ctx)
		return ptr.Of(response)
	})
	tt := assert.NewTracker[string](t)
	col2.Register(TrackerHandler[string](tt))
	tt.WaitOrdered("add/foo")

	response = "bar"
	rt.TriggerRecomputation()
	tt.WaitUnordered("delete/foo", "add/bar")

	response = "baz"
	rt.TriggerRecomputation()
	tt.WaitUnordered("delete/bar", "add/baz")
}
