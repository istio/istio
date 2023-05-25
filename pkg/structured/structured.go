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

package structured

import (
	"fmt"
)

// Error represents structured error information, for optional use in scope.X or log.X calls.
// It is not the same thing as structured logging. The "structured" here means adding a structure to user facing
// messages.
// See https://docs.google.com/document/d/1vdYswLQuYnrLA2fDjk6OoZx2flBABa18UjCGTn8gsg8/ for additional information.
type Error struct {
	// MoreInfo is additional information about the error e.g. a link to context describing the context for the error.
	MoreInfo string
	// Impact is the likely impact of the error on system function e.g. "Proxies are unable to communicate with Istiod."
	Impact string
	// Action is the next step the user should take e.g. "Open an issue or bug report."
	Action string
	// LikelyCause is the likely cause for the error e.g. "Software bug."
	LikelyCause string
	// Err is the original error string.
	Err error
}

// Error implements the error#Error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("\tmoreInfo=%s impact=%s action=%s likelyCause=%s err=%v",
		e.MoreInfo, e.Impact, e.Action, e.LikelyCause, e.Err)
}

// NewErr creates a new copy of an Error with the content of serr and err and returns a ptr to it.
func NewErr(serr *Error, err error) *Error {
	// Make a copy so that dictionary entry is not modified.
	ne := *serr
	ne.Err = err
	return &ne
}

// Unwrap implements error unwrapping for %w verb.
func (e *Error) Unwrap() error { return e }
