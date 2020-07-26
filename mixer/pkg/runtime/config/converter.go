//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package config

import (
	"fmt"

	"github.com/gogo/protobuf/types"
)

func toDictionary(p *types.Struct) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	for k, v := range p.Fields {
		var err error
		if result[k], err = toGoValue(v); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func toGoValue(v *types.Value) (interface{}, error) {
	if v != nil {
		switch k := v.Kind.(type) {

		case *types.Value_NullValue:
			return nil, nil

		case *types.Value_StringValue:
			return k.StringValue, nil

		case *types.Value_BoolValue:
			return k.BoolValue, nil

		case *types.Value_NumberValue:
			return k.NumberValue, nil

		case *types.Value_StructValue:
			return toDictionary(k.StructValue)

		case *types.Value_ListValue:
			var lst []interface{}
			for _, e := range k.ListValue.Values {
				i, err := toGoValue(e)
				if err != nil {
					return nil, err
				}
				lst = append(lst, i)
			}
			return lst, nil
		}
	}

	return nil, fmt.Errorf("error serializing struct value: %v", v)
}
