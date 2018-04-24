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

package util

// Error encapsulates the short and long errors.
type Error struct {
	// ShortMessage is the short string for the error.
	ShortMessage string
	// FullError is the full error.
	FullError error
}

// NewError creates a new Error instance.
func NewError(short string, full error) *Error {
	return &Error{
		ShortMessage: short,
		FullError:    full,
	}
}
