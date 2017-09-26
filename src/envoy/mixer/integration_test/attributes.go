// Copyright 2017 Istio Authors. All Rights Reserved.
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

package test

import (
	"encoding/json"
	"fmt"
	"reflect"

	"istio.io/mixer/pkg/attribute"
)

func verifyStringMap(actual map[string]string, expected map[string]interface{}) error {
	for k, v := range expected {
		vstring := v.(string)
		// "-" make sure the key does not exist.
		if vstring == "-" {
			if _, ok := actual[k]; ok {
				return fmt.Errorf("key %+v is NOT expected", k)
			}
		} else {
			if val, ok := actual[k]; ok {
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
func Verify(b *attribute.MutableBag, json_results string) error {
	var r map[string]interface{}
	if err := json.Unmarshal([]byte(json_results), &r); err != nil {
		return fmt.Errorf("unable to decode json %v", err)
	}

	all_keys := make(map[string]bool)
	for _, k := range b.Names() {
		all_keys[k] = true
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
				        val_string := fmt.Sprintf("%v", val)
					if val_string != v.(string) {
						return fmt.Errorf("attribute %+v value doesn't match. Actual %+v, expected %+v",
							k, val_string, v.(string))
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
		case map[string]interface{}:
			if val, ok := b.Get(k); ok {
				if err := verifyStringMap(val.(map[string]string), v.(map[string]interface{})); err != nil {
					return fmt.Errorf("attribute %+v StringMap doesn't match: %+v", k, err)
				}
			} else {
				return fmt.Errorf("attribute %+v is expected", k)
			}
		default:
			return fmt.Errorf("attribute %+v is of a type %+v that I don't know how to handle ",
				k, reflect.TypeOf(v))
		}
		delete(all_keys, k)

	}

	if len(all_keys) > 0 {
		var s string
		for k, _ := range all_keys {
			s += k + ", "
		}
		return fmt.Errorf("Following attributes are not expected: %s", s)
	}
	return nil
}
