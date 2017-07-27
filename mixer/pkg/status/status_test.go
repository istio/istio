// Copyright 2017 Istio Authors
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

package status

import (
	"errors"
	"testing"

	"github.com/gogo/protobuf/proto"
	rpc "github.com/googleapis/googleapis/google/rpc"
	multierror "github.com/hashicorp/go-multierror"
)

func TestStatus(t *testing.T) {
	if !IsOK(OK) {
		t.Error("Expecting the OK status to actually be, well, you know, OK...")
	}

	s := New(rpc.ABORTED)
	if s.Code != int32(rpc.ABORTED) {
		t.Errorf("Got %v, expected rpc.ABORTED", s.Code)
	}

	s = WithMessage(rpc.ABORTED, "Aborted!")
	if s.Code != int32(rpc.ABORTED) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.ABORTED Aborted!", s.Code, s.Message)
	}

	s = WithError(errors.New("aborted"))
	if s.Code != int32(rpc.INTERNAL) || s.Message != "aborted" {
		t.Errorf("Got %v %v, expected rpc.INTERNAL aborted", s.Code, s.Message)
	}

	s = WithInternal("Aborted!")
	if s.Code != int32(rpc.INTERNAL) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.INTERNAL Aborted!", s.Code, s.Message)
	}

	s = WithCancelled("Aborted!")
	if s.Code != int32(rpc.CANCELLED) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.CANCELLED Aborted!", s.Code, s.Message)
	}

	s = WithPermissionDenied("Aborted!")
	if s.Code != int32(rpc.PERMISSION_DENIED) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.PERMISSION_DENIED Aborted!", s.Code, s.Message)
	}

	s = WithInvalidArgument("Aborted!")
	if s.Code != int32(rpc.INVALID_ARGUMENT) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.INVALID_ARGUMENT Aborted!", s.Code, s.Message)
	}

	s = WithResourceExhausted("Aborted!")
	if s.Code != int32(rpc.RESOURCE_EXHAUSTED) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.RESOURCE_EXHAUSTED Aborted!", s.Code, s.Message)
	}

	s = WithDeadlineExceeded("Aborted!")
	if s.Code != int32(rpc.DEADLINE_EXCEEDED) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.DEADLINE_EXCEEDED Aborted!", s.Code, s.Message)
	}

	s = InvalidWithDetails("Invalid", NewBadRequest("test", errors.New("error")))
	if s.Code != int32(rpc.INVALID_ARGUMENT) && s.Message != "Invalid" && len(s.Details) != 1 {
		t.Errorf("Got %v, expected status with code = rpc.INVALID_ARGUMENT and populated details", s)
	}
}

func TestNewBadRequest(t *testing.T) {
	me := multierror.Append(errors.New("error one"), errors.New("error two"))

	cases := []struct {
		name  string
		field string
		err   error
		want  *rpc.BadRequest
	}{
		{"simple error", "field", errors.New("error"), newBadReq(newViolation("error"))},
		{"go-multierror", "field", me, newBadReq(newViolation("error one"), newViolation("error two"))},
	}
	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			got := NewBadRequest(v.field, v.err)
			if !proto.Equal(got, v.want) {
				t.Fatalf("Got %v, want %v", got, v.want)
			}
		})
	}
}

func newViolation(desc string) *rpc.BadRequest_FieldViolation {
	return &rpc.BadRequest_FieldViolation{Field: "field", Description: desc}
}

func newBadReq(violations ...*rpc.BadRequest_FieldViolation) *rpc.BadRequest {
	return &rpc.BadRequest{FieldViolations: violations}
}
