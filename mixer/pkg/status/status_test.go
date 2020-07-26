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

package status

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/types"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"

	"istio.io/api/policy/v1beta1"
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

	s = WithUnknown("Aborted!")
	if s.Code != int32(rpc.UNKNOWN) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.UNKNOWN Aborted!", s.Code, s.Message)
	}

	s = WithNotFound("Aborted!")
	if s.Code != int32(rpc.NOT_FOUND) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.NOT_FOUND Aborted!", s.Code, s.Message)
	}

	s = WithAlreadyExists("Aborted!")
	if s.Code != int32(rpc.ALREADY_EXISTS) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.ALREADY_EXISTS Aborted!", s.Code, s.Message)
	}

	s = WithFailedPrecondition("Aborted!")
	if s.Code != int32(rpc.FAILED_PRECONDITION) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.FAILED_PRECONDITION Aborted!", s.Code, s.Message)
	}

	s = WithAborted("Aborted!")
	if s.Code != int32(rpc.ABORTED) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.ABORTED Aborted!", s.Code, s.Message)
	}

	s = WithOutOfRange("Aborted!")
	if s.Code != int32(rpc.OUT_OF_RANGE) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.OUT_OF_RANGE Aborted!", s.Code, s.Message)
	}

	s = WithUnimplemented("Aborted!")
	if s.Code != int32(rpc.UNIMPLEMENTED) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.UNIMPLEMENTED Aborted!", s.Code, s.Message)
	}

	s = WithUnavailable("Aborted!")
	if s.Code != int32(rpc.UNAVAILABLE) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.UNAVAILABLE Aborted!", s.Code, s.Message)
	}

	s = WithOutOfRange("Aborted!")
	if s.Code != int32(rpc.OUT_OF_RANGE) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.OUT_OF_RANGE Aborted!", s.Code, s.Message)
	}

	s = WithDataLoss("Aborted!")
	if s.Code != int32(rpc.DATA_LOSS) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.DATA_LOSS Aborted!", s.Code, s.Message)
	}

	s = WithUnauthenticated("Aborted!")
	if s.Code != int32(rpc.UNAUTHENTICATED) || s.Message != "Aborted!" {
		t.Errorf("Got %v %v, expected rpc.UNAUTHENTICATED Aborted!", s.Code, s.Message)
	}

	msg := String(s)
	if msg == "" {
		t.Errorf("Expecting valid string, got nothing")
	}

	s = rpc.Status{Code: -123}
	msg = String(s)
	if msg == "" {
		t.Errorf("Expecting valid string, got nothing")
	}

	s = OK
	msg = String(s)
	if msg == "" {
		t.Errorf("Expecting valid string, got nothing")
	}
}

func TestErrorDetail(t *testing.T) {
	if response := GetDirectHTTPResponse(OK); response != nil {
		t.Errorf("GetDirectHTTPResponse(OK) => got %#v, want nil", response)
	}

	response := &v1beta1.DirectHttpResponse{
		Code:    v1beta1.MovedPermanently,
		Body:    "istio.io/api",
		Headers: map[string]string{"location": "istio.io/api"},
	}
	any := PackErrorDetail(response)

	s := rpc.Status{Code: int32(rpc.UNAUTHENTICATED), Details: []*types.Any{
		nil,
		{TypeUrl: "types.google.com/istio.policy.v1beta1.DirectHttpResponse", Value: []byte{1}},
		any,
	}}

	if got := GetDirectHTTPResponse(s); !reflect.DeepEqual(got, response) {
		t.Errorf("GetDirectHTTPResponse => got %#v, want %#v", got, response)
	}
}

func TestStatusCode(t *testing.T) {
	for code := range rpc.Code_name {
		httpCode := HTTPStatusFromCode(rpc.Code(code))
		if httpCode < 200 || httpCode > 600 {
			t.Errorf("unexpected HTTP code after translation: %d", httpCode)
		}
	}

	if code := HTTPStatusFromCode(rpc.Code(-1)); code != 500 {
		t.Errorf("unexpected undefined HTTP code: %d", code)
	}
}
