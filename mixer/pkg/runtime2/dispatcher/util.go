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

package dispatcher

import (
	"fmt"

	"istio.io/istio/mixer/pkg/attribute"
)

// getIdentityAttributeValue from the attribute bag, based on the id attribute.
func getIdentityAttributeValue(attrs attribute.Bag, idAttribute string) (string, error) {

	v, ok := attrs.Get(idAttribute)
	if !ok {
		return "", fmt.Errorf("identity parameter not found: '%s'", idAttribute)
	}

	var destination string
	if destination, ok = v.(string); !ok {
		return "", fmt.Errorf("identity parameter is not a string: '%s'", idAttribute)
	}

	return destination, nil
}

// getNamespace returns the namespace portion of a given fully qualified destination name. If the destination
// namespace cannot be deduced, then empty string is returned.
func getNamespace(destination string) string {
	l := len(destination)

	idx1 := -1
	for i := 0; i < l; i++ {
		if destination[i] == '.' {
			idx1 = i
			break
		}
	}
	if idx1 == -1 {
		return ""
	}

	idx2 := -1
	for i := idx1 + 1; i < l; i++ {
		if destination[i] == '.' {
			idx2 = i
			break
		}
	}

	if idx2 == -1 {
		idx2 = len(destination)
	}

	return destination[idx1+1 : idx2]
}
