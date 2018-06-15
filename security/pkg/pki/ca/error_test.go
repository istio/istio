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

package ca

import (
	"fmt"
	"testing"
)

func TestError(t *testing.T) {
	testCases := map[string]struct {
		eType   ErrType
		err     error
		message string
	}{
		"CA_NOT_READY": {
			eType:   CANotReady,
			err:     fmt.Errorf("test error1"),
			message: "CA_NOT_READY",
		},
		"CSR_ERROR": {
			eType:   CSRError,
			err:     fmt.Errorf("test error2"),
			message: "CSR_ERROR",
		},
		"TTL_ERROR": {
			eType:   TTLError,
			err:     fmt.Errorf("test error3"),
			message: "TTL_ERROR",
		},
		"CERT_GEN_ERROR": {
			eType:   CertGenError,
			err:     fmt.Errorf("test error4"),
			message: "CERT_GEN_ERROR",
		},
		"UNKNOWN": {
			eType:   -1,
			err:     fmt.Errorf("test error5"),
			message: "UNKNOWN",
		},
	}

	for k, tc := range testCases {
		caErr := NewError(tc.eType, tc.err)
		if caErr.Error() != tc.err.Error() {
			t.Errorf("[%s] unexpected error: '%s' VS (expected)'%s'", k, caErr.Error(), tc.err.Error())
		}
		if caErr.ErrorType() != tc.message {
			t.Errorf("[%s] unexpected error type message: '%s' VS (expected)'%s'", k, caErr.ErrorType(), tc.message)
		}
	}
}
