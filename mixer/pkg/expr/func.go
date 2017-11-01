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

// FuncBase defines the interface that every expression function must implement.
type FuncBase interface {
	// Name uniquely identifies the function.
	Name() string

	// ReturnType specifies the return type of this function.
	ReturnType() config.ValueType

	// ArgTypes specifies the argument types in order expected by the function.
	ArgTypes() []config.ValueType
}

// baseFunc is basetype for many funcs
type baseFunc struct {
	name         string
	argTypes     []config.ValueType
	retType      config.ValueType
	acceptsNulls bool
}

func (f *baseFunc) Name() string                 { return f.name }
func (f *baseFunc) ReturnType() config.ValueType { return f.retType }
func (f *baseFunc) ArgTypes() []config.ValueType { return f.argTypes }

func inventory() []FuncBase {
	return []FuncBase{
		&baseFunc{
			name:     "EQ",
			retType:  config.BOOL,
			argTypes: []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED},
		},
		&baseFunc{
			name:     "NEQ",
			retType:  config.BOOL,
			argTypes: []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED},
		},
		&baseFunc{
			name:     "OR",
			retType:  config.VALUE_TYPE_UNSPECIFIED,
			argTypes: []config.ValueType{config.VALUE_TYPE_UNSPECIFIED, config.VALUE_TYPE_UNSPECIFIED},
		},
		&baseFunc{
			name:     "LOR",
			retType:  config.BOOL,
			argTypes: []config.ValueType{config.BOOL, config.BOOL},
		},
		&baseFunc{
			name:     "LAND",
			retType:  config.BOOL,
			argTypes: []config.ValueType{config.BOOL, config.BOOL},
		},
		&baseFunc{
			name:     "INDEX",
			retType:  config.STRING,
			argTypes: []config.ValueType{config.STRING_MAP, config.STRING},
		},
		&baseFunc{
			name:     "ip",
			retType:  config.IP_ADDRESS,
			argTypes: []config.ValueType{config.STRING},
		},
		&baseFunc{
			name:     "timestamp",
			retType:  config.TIMESTAMP,
			argTypes: []config.ValueType{config.STRING},
		},
		&baseFunc{
			name:     "match",
			retType:  config.BOOL,
			argTypes: []config.ValueType{config.STRING, config.STRING},
		},
	}
}

// FuncMap provides inventory of available functions.
func FuncMap() map[string]FuncBase {
	m := make(map[string]FuncBase)
	for _, fn := range inventory() {
		m[fn.Name()] = fn
	}
	return m
}
