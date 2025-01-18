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

package nodeagent

import (
	"errors"
	"fmt"
)

var ErrRetryablePartialAdd = errors.New("partial add error")

type RetryablePartialAddError struct {
	inner error
}

func (e *RetryablePartialAddError) Error() string {
	return fmt.Sprintf("%s: %v", ErrRetryablePartialAdd.Error(), e.inner)
}

func (e *RetryablePartialAddError) Unwrap() []error {
	return []error{ErrRetryablePartialAdd, e.inner}
}

func NewErrRetryablePartialAdd(err error) *RetryablePartialAddError {
	return &RetryablePartialAddError{
		inner: err,
	}
}
