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
	"errors"
	"net/http"
	"reflect"
	"testing"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	tpb "istio.io/api/mixer/adapter/model/v1beta1"
	mixerpb "istio.io/api/mixer/v1"
	descriptor "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/pkg/attribute"
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

func TestDirectResponse(t *testing.T) {
	testcases := []struct {
		desc      string
		status    rpc.Status
		response  *descriptor.DirectHttpResponse
		directive *mixerpb.RouteDirective
	}{
		{
			desc:     "status success, no directives",
			status:   rpc.Status{Code: int32(rpc.OK)},
			response: &descriptor.DirectHttpResponse{},
			directive: &mixerpb.RouteDirective{
				DirectResponseCode: http.StatusOK,
			},
		},
		{
			desc:     "fallback to RPC response code (translated to http)",
			status:   rpc.Status{Code: int32(rpc.UNAUTHENTICATED)},
			response: &descriptor.DirectHttpResponse{},
			directive: &mixerpb.RouteDirective{
				DirectResponseCode: http.StatusUnauthorized,
			},
		},
		{
			desc:   "response status has precedence over RPC",
			status: rpc.Status{Code: int32(rpc.UNIMPLEMENTED)},
			response: &descriptor.DirectHttpResponse{
				Code: http.StatusMovedPermanently,
			},
			directive: &mixerpb.RouteDirective{
				DirectResponseCode: http.StatusMovedPermanently,
			},
		},
		{
			desc: "body is set",
			response: &descriptor.DirectHttpResponse{
				Code: http.StatusOK,
				Body: "OK!",
			},
			directive: &mixerpb.RouteDirective{
				DirectResponseCode: http.StatusOK,
				DirectResponseBody: "OK!",
			},
		},
		{
			desc: "headers added",
			response: &descriptor.DirectHttpResponse{
				Code:    http.StatusMovedPermanently,
				Headers: map[string]string{"Location": "URL"},
			},
			directive: &mixerpb.RouteDirective{
				DirectResponseCode: http.StatusMovedPermanently,
				ResponseHeaderOperations: []mixerpb.HeaderOperation{
					{
						Operation: mixerpb.REPLACE,
						Name:      "Location",
						Value:     "URL",
					},
				},
			},
		},
		{
			desc: "multiline set-cookie header",
			response: &descriptor.DirectHttpResponse{
				Code:    http.StatusMovedPermanently,
				Headers: map[string]string{"Location": "URL", "Set-Cookie": "c1=1; Secure; HttpOnly;,c2=2"},
			},
			directive: &mixerpb.RouteDirective{
				DirectResponseCode: http.StatusMovedPermanently,
				ResponseHeaderOperations: []mixerpb.HeaderOperation{
					{
						Operation: mixerpb.REPLACE,
						Name:      "Location",
						Value:     "URL",
					},
					{
						Operation: mixerpb.APPEND,
						Name:      "Set-Cookie",
						Value:     "c1=1; Secure; HttpOnly;",
					},
					{
						Operation: mixerpb.APPEND,
						Name:      "Set-Cookie",
						Value:     "c2=2",
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		s := &session{}
		s.handleDirectResponse(tc.status, tc.response)
		if s.checkResult.RouteDirective.DirectResponseCode != tc.directive.DirectResponseCode ||
			s.checkResult.RouteDirective.DirectResponseBody != tc.directive.DirectResponseBody ||
			!equalHeaderOperations(s.checkResult.RouteDirective.RequestHeaderOperations, tc.directive.RequestHeaderOperations) ||
			!equalHeaderOperations(s.checkResult.RouteDirective.ResponseHeaderOperations, tc.directive.ResponseHeaderOperations) {
			t.Fatalf("route directive mismatch in %s '%+v' != '%+v'", tc.desc, s.checkResult.RouteDirective, tc.directive)
		}
	}
}

func equalHeaderOperations(actual, expected []mixerpb.HeaderOperation) bool {
	if len(actual) != len(expected) {
		return false
	}
	delta := make(map[mixerpb.HeaderOperation]int)

	for _, ex := range expected {
		delta[ex]++
	}
	for _, h := range actual {
		if _, ok := delta[h]; !ok {
			return false
		}
		delta[h]--
		if delta[h] == 0 {
			delete(delta, h)
		}
	}
	return len(delta) == 0
}
