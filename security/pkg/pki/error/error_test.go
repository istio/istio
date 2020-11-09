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

package error

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
)

func TestError(t *testing.T) {
	testCases := map[string]struct {
		eType   ErrType
		err     error
		message string
		code    codes.Code
	}{
		"CA_NOT_READY": {
			eType:   CANotReady,
			err:     fmt.Errorf("test error1"),
			message: "CA_NOT_READY",
			code:    codes.Internal,
		},
		"CSR_ERROR": {
			eType:   CSRError,
			err:     fmt.Errorf("test error2"),
			message: "CSR_ERROR",
			code:    codes.InvalidArgument,
		},
		"TTL_ERROR": {
			eType:   TTLError,
			err:     fmt.Errorf("test error3"),
			message: "TTL_ERROR",
			code:    codes.InvalidArgument,
		},
		"CERT_GEN_ERROR": {
			eType:   CertGenError,
			err:     fmt.Errorf("test error4"),
			message: "CERT_GEN_ERROR",
			code:    codes.Internal,
		},
		"UNKNOWN": {
			eType:   -1,
			err:     fmt.Errorf("test error5"),
			message: "UNKNOWN",
			code:    codes.Internal,
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
		if caErr.HTTPErrorCode() != tc.code {
			t.Errorf("[%s] unexpected error HTTP code: '%d' VS (expected)'%d'", k, caErr.HTTPErrorCode(), tc.code)
		}
	}
}
