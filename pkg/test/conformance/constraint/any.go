// Copyright 2019 Istio Authors
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

package constraint

import "fmt"

// Any item must match check Constraints
type Any struct {
	Constraints []Check
}

var _ Range = &Any{}

// ValidateItems implements Range
func (e *Any) ValidateItems(arr []interface{}, p Params) error {
	var matches int
mainloop:
	for _, a := range arr {
		for _, c := range e.Constraints {
			err := c.ValidateItem(a, p)
			if err != nil {
				continue mainloop
			}
		}
		matches++
	}

	switch matches {
	case 0:
		return fmt.Errorf("no item matched constraints: %v", arr)
	default:
		return nil
	}
}
