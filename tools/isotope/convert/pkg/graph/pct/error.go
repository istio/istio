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

package pct

import "fmt"

// InvalidPercentageStringError is returned when parsing an invalid percentage
// string.
type InvalidPercentageStringError struct {
	String string
}

func (e InvalidPercentageStringError) Error() string {
	return fmt.Sprintf(
		"invalid percentage as string: %v (must be between \"0%%\" and \"100%%\")",
		e.String)
}

// OutOfRangeError is returned when parsing a percentage that is out of range.
type OutOfRangeError struct {
	Float float64
}

func (e OutOfRangeError) Error() string {
	return fmt.Sprintf(
		"percentage %v is out of range (must be between 0.0 and 1.0)",
		e.Float)
}
