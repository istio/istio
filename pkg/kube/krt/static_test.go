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
	"istio.io/istio/pkg/test/util/assert"
)

func TestStaticCollection(t *testing.T) {
	opts := testOptions(t)
	c := krt.NewStaticCollection[Named](nil, []Named{{"ns", "a"}}, opts.WithName("c")...)
	assert.Equal(t, c.Synced().HasSynced(), true, "should start synced")
	assert.Equal(t, c.List(), []Named{{"ns", "a"}})

	tracker := assert.NewTracker[string](t)
	c.RegisterBatch(BatchedTrackerHandler[Named](tracker), true)
	tracker.WaitOrdered("add/ns/a")

	c.UpdateObject(Named{"ns", "b"})
	tracker.WaitOrdered("add/ns/b")

	c.UpdateObject(Named{"ns", "b"})
	tracker.WaitOrdered("update/ns/b")

	tracker2 := assert.NewTracker[string](t)
	c.RegisterBatch(BatchedTrackerHandler[Named](tracker2), true)
	tracker2.WaitCompare(CompareUnordered("add/ns/a", "add/ns/b"))

	c.DeleteObject("ns/b")
	tracker.WaitOrdered("delete/ns/b")
}
