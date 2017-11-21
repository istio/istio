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

package expr

import (
	config "istio.io/api/mixer/v1/config/descriptor"
)

// FunctionMetadata contains type metadata for functions.
type FunctionMetadata struct {
	// Name is the name of the function.
	Name string

	// ReturnType is the return type of the function.
	ReturnType config.ValueType

	// ArgumentTypes is the types of the arguments in the order that is expected by the function.
	ArgumentTypes []config.ValueType
}

func intrinsics() []FunctionMetadata {
	return []FunctionMetadata{
		{
			Name:          "EQ",
			ReturnType:    config.BOOL,
			ArgumentTypes: []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED},
		},
		{
			Name:          "NEQ",
			ReturnType:    config.BOOL,
			ArgumentTypes: []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED},
		},
		{
			Name:          "OR",
			ReturnType:    config.VALUE_TYPE_UNSPECIFIED,
			ArgumentTypes: []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED},
		},
		{
			Name:          "LOR",
			ReturnType:    config.BOOL,
			ArgumentTypes: []config.ValueType{config.BOOL, config.BOOL},
		},
		{
			Name:          "LAND",
			ReturnType:    config.BOOL,
			ArgumentTypes: []config.ValueType{config.BOOL, config.BOOL},
		},
		{
			Name:          "INDEX",
			ReturnType:    config.STRING,
			ArgumentTypes: []config.ValueType{config.STRING_MAP, config.STRING},
		},
	}
}

// FuncMap generates a full function map, combining the intrinsic functions needed for type-checking,
// along with external functions that are supplied as the functions parameter.
func FuncMap(functions []FunctionMetadata) map[string]FunctionMetadata {
	m := make(map[string]FunctionMetadata)
	for _, fn := range intrinsics() {
		m[fn.Name] = fn
	}
	for _, fn := range functions {
		m[fn.Name] = fn
	}
	return m
}
