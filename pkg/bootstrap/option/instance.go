// Copyright Istio Authors
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

package option

import (
	"reflect"

	"google.golang.org/protobuf/types/known/durationpb"

	networkingAPI "istio.io/api/networking/v1alpha3"
)

// NewTemplateParams creates a new golang template parameter map from the given list of options.
func NewTemplateParams(is ...Instance) (map[string]any, error) {
	params := make(map[string]any)

	for _, i := range is {
		if err := i.apply(params); err != nil {
			return nil, err
		}
	}

	return params, nil
}

// Unique name for an option.
type Name string

func (n Name) String() string {
	return string(n)
}

// Instance of a bootstrap option.
type Instance interface {
	Name() Name

	// apply this option to the given template parameter map.
	apply(map[string]any) error
}

var _ Instance = &instance{}

type (
	convertFunc func(*instance) (any, error)
	applyFunc   func(map[string]any, *instance) error
)

type instance struct {
	name      Name
	convertFn convertFunc
	applyFn   applyFunc
}

func (i *instance) Name() Name {
	return i.name
}

func (i *instance) withConvert(fn convertFunc) *instance {
	out := *i
	out.convertFn = fn
	return &out
}

func (i *instance) apply(params map[string]any) error {
	return i.applyFn(params, i)
}

func newOption(name Name, value any) *instance {
	return &instance{
		name: name,
		convertFn: func(i *instance) (any, error) {
			return value, nil
		},
		applyFn: func(params map[string]any, o *instance) error {
			convertedValue, err := o.convertFn(o)
			if err != nil {
				return err
			}
			params[o.name.String()] = convertedValue
			return nil
		},
	}
}

// skipOption creates a placeholder option that will not be applied to the output template map.
func skipOption(name Name) *instance {
	return &instance{
		name: name,
		convertFn: func(*instance) (any, error) {
			return nil, nil
		},
		applyFn: func(map[string]any, *instance) error {
			// Don't apply the option.
			return nil
		},
	}
}

func newStringArrayOptionOrSkipIfEmpty(name Name, value []string) *instance {
	if len(value) == 0 {
		return skipOption(name)
	}
	return newOption(name, value)
}

func newOptionOrSkipIfZero(name Name, value any) *instance {
	v := reflect.ValueOf(value)
	if v.IsZero() {
		return skipOption(name)
	}
	return newOption(name, value)
}

func newDurationOption(name Name, value *durationpb.Duration) *instance {
	return newOptionOrSkipIfZero(name, value).withConvert(durationConverter(value))
}

func newTCPKeepaliveOption(name Name, value *networkingAPI.ConnectionPoolSettings_TCPSettings_TcpKeepalive) *instance {
	return newOptionOrSkipIfZero(name, value).withConvert(keepaliveConverter(value))
}
