// Copyright 2018 Istio Authors
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
	"errors"
	"reflect"
	"testing"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
)

func TestSessionPool(t *testing.T) {
	d := New(nil, false)

	// Prime the pool
	sessions := make([]*session, 100)
	for i := 0; i < 100; i++ {
		s := d.getSession(context.TODO(), 0, nil)
		sessions[i] = s
	}
	for i := 0; i < 100; i++ {
		d.putSession(sessions[i])
	}

	// test cleaning
	for i := 0; i < 100; i++ {
		s := d.getSession(context.TODO(), 0, nil)
		s.activeDispatches = 53 + i
		sessions[i] = s
	}
	for i := 0; i < 100; i++ {
		d.putSession(sessions[i])
	}

	for i := 0; i < 100; i++ {
		s := d.getSession(context.TODO(), 0, nil)

		// all fields should be clean, except for these two
		expected := &session{
			impl: d,
			rc:   d.rc,
			ctx:  context.TODO(),
		}

		if !reflect.DeepEqual(s, expected) {
			t.Fatalf("session mismatch '%+v' != '%+v'", s, expected)
		}
	}
}

func TestSession_Clear(t *testing.T) {
	s := &session{
		impl:             New(nil, false),
		activeDispatches: 23,
		bag:              attribute.GetMutableBag(nil),
		completed:        make(chan *dispatchState, 10),
		err:              errors.New("some error"),
		ctx:              context.TODO(),
		checkResult:      adapter.CheckResult{ValidUseCount: 53},
		quotaResult:      adapter.QuotaResult{Amount: 23},
		quotaArgs:        QuotaMethodArgs{BestEffort: true},
		variety:          tpb.TEMPLATE_VARIETY_CHECK,
		responseBag:      attribute.GetMutableBag(nil),
	}

	s.clear()

	// check s.completed separately, as reflect.DeepEqual doesn't deal with it well.
	if s.completed == nil {
		t.Fail()
	}
	s.completed = nil

	expected := &session{}

	if !reflect.DeepEqual(s, expected) {
		t.Fatalf("'%+v' != '%+v'", s, expected)
	}
}

func TestSession_Clear_LeftOverWork(t *testing.T) {
	s := &session{
		completed: make(chan *dispatchState, 10),
	}

	s.completed <- &dispatchState{}
	s.clear()

	select {
	case <-s.completed:
		t.Fatal("Channel should have been drained")
	default:
	}
}

func TestSession_EnsureParallelism(t *testing.T) {
	s := &session{
		completed: make(chan *dispatchState, 10),
	}

	s.ensureParallelism(5)
	if cap(s.completed) != 10 {
		t.Fail()
	}

	s.ensureParallelism(11)
	if cap(s.completed) < 11 {
		t.Fail()
	}
}
