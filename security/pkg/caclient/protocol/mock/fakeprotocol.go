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

package mock

import (
	"fmt"

	pb "istio.io/istio/security/proto"
)

const (
	maxCAClientSuccessReturns = 8
)

// FakeProtocol is a fake for testing, implements CAProtocol interface.
type FakeProtocol struct {
	counter int
	resp    *pb.CsrResponse
	errMsg  string
}

// NewFakeProtocol returns a FakeProtocol with configured response and expected error.
func NewFakeProtocol(response *pb.CsrResponse, err string) *FakeProtocol {
	return &FakeProtocol{
		resp:   response,
		errMsg: err,
	}
}

// SendCSR returns the result based on the predetermined config.
func (f *FakeProtocol) SendCSR(req *pb.CsrRequest) (*pb.CsrResponse, error) {
	f.counter++
	if f.counter > maxCAClientSuccessReturns {
		return nil, fmt.Errorf("terminating the test with errors")
	}

	if f.errMsg != "" {
		return nil, fmt.Errorf(f.errMsg)
	}
	if f.resp == nil {
		return &pb.CsrResponse{}, nil
	}
	return f.resp, nil
}

// InvokeTimes returns the times that SendCSR has been invoked.
func (f *FakeProtocol) InvokeTimes() int {
	return f.counter
}
