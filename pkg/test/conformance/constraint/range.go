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
	"encoding/json"
	"fmt"
)

// Range is a check against a group of items.
type Range interface {
	ValidateItems(arr []interface{}, p Params) error
}

func parseRange(rb json.RawMessage) (Range, error) {
	m := make(map[string]interface{})
	if err := json.Unmarshal(rb, &m); err != nil {
		return nil, err
	}

	_, found := m["empty"]
	if found {
		return &Empty{}, nil
	}

	_, found = m["exactlyOne"]
	if found {
		eo := struct {
			ExactlyOne []json.RawMessage `json:"exactlyOne"`
		}{}
		if err := json.Unmarshal(rb, &eo); err != nil {
			return nil, err
		}
		constraints, err := parseChecks(eo.ExactlyOne)
		if err != nil {
			return nil, err
		}

		return &ExactlyOne{
			Constraints: constraints,
		}, nil
	}

	_, found = m["any"]
	if found {
		a := struct {
			Any []json.RawMessage `json:"any"`
		}{}
		if err := json.Unmarshal(rb, &a); err != nil {
			return nil, err
		}
		constraints, err := parseChecks(a.Any)
		if err != nil {
			return nil, err
		}

		return &Any{
			Constraints: constraints,
		}, nil
	}

	return nil, fmt.Errorf("no recognized range-checks found")
}
