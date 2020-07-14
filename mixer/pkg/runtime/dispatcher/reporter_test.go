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
	"context"
	"reflect"
	"testing"

	"istio.io/istio/mixer/pkg/runtime/routing"
)

func TestReporterPool(t *testing.T) {
	ctx := context.TODO()

	d := New(nil, false)

	// Prime the pool
	reporters := make([]*reporter, 100)
	for i := 0; i < 100; i++ {
		r := d.getReporter(context.TODO())
		reporters[i] = r
	}
	for i := 0; i < 100; i++ {
		d.putReporter(reporters[i])
	}

	// test cleaning
	for i := 0; i < 100; i++ {
		r := d.getReporter(ctx)
		reporters[i] = r
	}
	for i := 0; i < 100; i++ {
		d.putReporter(reporters[i])
	}

	expected := &reporter{
		impl:   d,
		rc:     d.rc,
		ctx:    ctx,
		states: make(map[*routing.Destination]*dispatchState),
	}

	for i := 0; i < 100; i++ {
		r := d.getReporter(ctx)
		if !reflect.DeepEqual(r, expected) {
			t.Fatalf("mismatch '%+v' != '%+v'", r, expected)
		}
	}
}

func TestReporter_Clear(t *testing.T) {
	r := &reporter{
		impl:   &Impl{},
		ctx:    context.TODO(),
		rc:     &RoutingContext{},
		states: make(map[*routing.Destination]*dispatchState),
	}

	r.states[&routing.Destination{}] = &dispatchState{}

	r.clear()

	expected := &reporter{
		states: make(map[*routing.Destination]*dispatchState),
	}

	if !reflect.DeepEqual(r, expected) {
		t.Fatalf("mismatch '%+v' != '%+v'", r, expected)
	}
}
