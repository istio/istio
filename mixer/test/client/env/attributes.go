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

package env

import (
	"encoding/json"
	"fmt"
	"reflect"

	"istio.io/istio/mixer/pkg/attribute"
)

// TODO: remove duplicated code by change StringMap object to expose the whole map
func verifyObjStringMap(actual attribute.StringMap, expected map[string]interface{}) error {
	for k, v := range expected {
		vstring := v.(string)
		// "-" make sure the key does not exist.
		if vstring == "-" {
			if _, ok := actual.Get(k); ok {
				return fmt.Errorf("key %+v is NOT expected", k)
			}
		} else {
			if val, ok := actual.Get(k); ok {
				// "*" only check key exist
				if val != vstring && vstring != "*" {
					return fmt.Errorf("key %+v value doesn't match. Actual %+v, expected %+v",
						k, val, vstring)
				}
			} else {
				return fmt.Errorf("key %+v is expected", k)
			}
		}
	}
	return nil
}

// Verify verifes an attributeBug with expected json string.
// Attributes verification rules:
//
// 1) If value is *,  key must exist, but value is not checked.
// 1) If value is -,  key must NOT exist.
// 3) At top level attributes, not inside StringMap, all keys must
//    be listed. Extra keys are NOT allowed
// 3) Inside StringMap, not need to list all keys. Extra keys are allowed
//
// Attributes provided from envoy config
// * source.id and source.namespace are forwarded from client proxy
// * target.id and target.namespace are from server proxy
//
// HTTP header "x-istio-attributes" is used to forward attributes between
// proxy. It should be removed before calling mixer and backend.
//
func Verify(b *attribute.MutableBag, expectedJSON string) error {
	var r map[string]interface{}
	if err := json.Unmarshal([]byte(expectedJSON), &r); err != nil {
		return fmt.Errorf("unable to decode json %v", err)
	}

	allKeys := make(map[string]bool)
	for _, k := range b.Names() {
		allKeys[k] = true
	}

	for k, v := range r {
		switch vv := v.(type) {
		case string:
			// "*" means only checking key.
			if vv == "*" {
				if _, ok := b.Get(k); !ok {
					return fmt.Errorf("attribute %+v is expected", k)
				}
			} else {
				if val, ok := b.Get(k); ok {
					strVal := fmt.Sprintf("%v", val)
					if strVal != v.(string) {
						return fmt.Errorf("attribute %+v value doesn't match. Actual %+v, expected %+v",
							k, strVal, v.(string))
					}
				} else {
					return fmt.Errorf("attribute %+v is expected", k)
				}
			}
		case float64:
			// Json converts all integers to float64,
			// Our tests only verify size related attributes which are int64 type
			if val, ok := b.Get(k); ok {
				vint64 := int64(vv)
				if val.(int64) != vint64 {
					return fmt.Errorf("attribute %+v value doesn't match. Actual %+v, expected %+v",
						k, val.(int64), vint64)
				}
			} else {
				return fmt.Errorf("attribute %+v is expected", k)
			}
		case bool:
			if val, ok := b.Get(k); ok {
				vbool := vv
				if val.(bool) != vbool {
					return fmt.Errorf("attribute %+v value doesn't match. Actual %+v, expected %+v",
						k, val.(bool), vbool)
				}
			} else {
				return fmt.Errorf("attribute %+v is expected", k)
			}
		case map[string]interface{}:
			if val, ok := b.Get(k); ok {
				var err error
				switch val.(type) {
				case attribute.StringMap:
					err = verifyObjStringMap(val.(attribute.StringMap), v.(map[string]interface{}))
				default:
					return fmt.Errorf("attribute %+v is of a unknown type %+v",
						k, reflect.TypeOf(val))
				}
				if err != nil {
					return fmt.Errorf("attribute %+v StringMap doesn't match: %+v", k, err)
				}
			} else {
				return fmt.Errorf("attribute %+v is expected", k)
			}
		default:
			return fmt.Errorf("attribute %+v is of a type %+v that I don't know how to handle ",
				k, reflect.TypeOf(v))
		}
		delete(allKeys, k)

	}

	if len(allKeys) > 0 {
		var s string
		for k := range allKeys {
			s += k + ", "
		}
		return fmt.Errorf("following attributes are not expected: %s", s)
	}
	return nil
}
