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

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
)

// ExactlyOne item in a collection must match all check Constraints
type ExactlyOne struct {
	Constraints []Check
}

var _ Range = &ExactlyOne{}

// ValidateItems implements Range
func (e *ExactlyOne) ValidateItems(arr []interface{}, p Params) error {
	var matches int
	var err error
mainloop:
	for _, a := range arr {
		for _, c := range e.Constraints {
			er := c.ValidateItem(a, p)
			if er != nil {
				err = multierror.Append(err, er)
				continue mainloop
			}
		}
		matches++
	}

	switch matches {
	case 0:
		err = multierror.Append(err, fmt.Errorf("no item matched constraints: %v", arr))
		return multierror.Flatten(err)
	case 1:
		return nil
	default:
		return fmt.Errorf("multiple items(%d) matched constraints: %v", matches, arr)
	}
}
