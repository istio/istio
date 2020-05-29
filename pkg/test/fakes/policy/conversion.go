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

package policy

import (
	"fmt"
	"reflect"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"

	"istio.io/istio/mixer/template/metric"
)

// Conversion from/to Any for various well-known types.

var knownProtoTypes = []reflect.Type{
	reflect.TypeOf(metric.InstanceMsg{}),
}

func toAny(p proto.Message) (*types.Any, error) {
	url := reflect.TypeOf(p).Elem().String()

	b, err := proto.Marshal(p)
	if err != nil {
		return nil, err
	}

	return &types.Any{
		Value:   b,
		TypeUrl: url,
	}, nil
}

func fromAny(a *types.Any) (proto.Message, error) {
	for _, t := range knownProtoTypes {
		if t.String() == a.TypeUrl {
			p := reflect.New(t).Interface().(proto.Message)
			err := proto.Unmarshal(a.Value, p)
			return p, err
		}
	}

	return nil, fmt.Errorf("unrecognized type url when unmarshalling Any: '%s'", a.TypeUrl)
}
