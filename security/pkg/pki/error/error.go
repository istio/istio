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

import "google.golang.org/grpc/codes"

// ErrType is the type for CA errors.
type ErrType int

const (
	// CANotReady means the CA is not ready to sign CSRs.
	CANotReady ErrType = iota
	// CSRError means the CA cannot sign CSR due to CSR error.
	CSRError
	// TTLError means the required TTL is invalid.
	TTLError
	// CertGenError means an error happened during the certificate generation.
	CertGenError
)

// Error encapsulates the short and long errors.
type Error struct {
	t   ErrType
	err error
}

// Error returns the string error message.
func (e Error) Error() string {
	return e.err.Error()
}

// ErrorType returns a short string representing the error type.
func (e Error) ErrorType() string {
	switch e.t {
	case CANotReady:
		return "CA_NOT_READY"
	case CSRError:
		return "CSR_ERROR"
	case TTLError:
		return "TTL_ERROR"
	case CertGenError:
		return "CERT_GEN_ERROR"
	}
	return "UNKNOWN"
}

// HTTPErrorCode returns an HTTP error code representing the error type.
func (e Error) HTTPErrorCode() codes.Code {
	switch e.t {
	case CANotReady:
		return codes.Internal
	case CertGenError:
		return codes.Internal
	case CSRError:
		return codes.InvalidArgument
	case TTLError:
		return codes.InvalidArgument
	}
	return codes.Internal
}

// NewError creates a new Error instance.
func NewError(t ErrType, err error) *Error {
	return &Error{
		t:   t,
		err: err,
	}
}
