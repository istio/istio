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

	rpc "github.com/gogo/googleapis/google/rpc"
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
}
