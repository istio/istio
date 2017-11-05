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

package runtime

import (
	"context"
	"reflect"
	"testing"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/attribute"
)

func TestNewContextWithRequestData(t *testing.T) {
	for _, tc := range []struct {
		name  string
		attrs map[string]interface{}
		want  *adapter.RequestData
	}{
		{
			name:  "attr bag empty",
			attrs: nil,
			want:  &adapter.RequestData{},
		},
		{
			name:  "attr contains destination.service",
			attrs: map[string]interface{}{"destination.service": "myservice-foo.bar.com"},
			want:  &adapter.RequestData{DestinationService: adapter.Service{FullName: "myservice-foo.bar.com"}},
		},
		{
			name:  "attr does not contain destination.service",
			attrs: nil,
			want:  &adapter.RequestData{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			requestBag := attribute.GetMutableBag(nil)
			for k, v := range tc.attrs {
				requestBag.Set(k, v)
			}

			gotReqData, _ := adapter.RequestDataFromContext(newContextWithRequestData(ctx, requestBag, DefaultIdentityAttribute))
			if !reflect.DeepEqual(gotReqData, tc.want) {
				t.Errorf("newContextWithRequestData with attrs '%v' => RequestData %v, want %v", tc.attrs, gotReqData, tc.want)
			}
		})
	}
}

// already contains reqdata
func TestNewContextWithRequestData_AlreadyContainsReqData(t *testing.T) {
	ctx := context.Background()
	requestBag := attribute.GetMutableBag(nil)

	requestBag.Set("destination.service", "one.com")
	gotReqData, _ := adapter.RequestDataFromContext(newContextWithRequestData(ctx, requestBag, DefaultIdentityAttribute))
	wantReqData := &adapter.RequestData{DestinationService: adapter.Service{FullName: "one.com"}}
	if !reflect.DeepEqual(gotReqData, wantReqData) {
		t.Errorf("TestNewContextWithRequestData_AlreadyContainsReqData with attribute '%v' => RequestData %v, "+
			"want %v", map[string]interface{}{"destination.service": "one.com"}, gotReqData, wantReqData)
	}

	requestBag.Set("destination.service", "two.com")
	gotReqData, _ = adapter.RequestDataFromContext(newContextWithRequestData(ctx, requestBag, DefaultIdentityAttribute))
	wantReqData = &adapter.RequestData{DestinationService: adapter.Service{FullName: "two.com"}}
	if !reflect.DeepEqual(gotReqData, wantReqData) {
		t.Errorf("TestNewContextWithRequestData_AlreadyContainsReqData with attribute '%v' => RequestData %v, "+
			"want %v", map[string]interface{}{"destination.service": "two.com"}, gotReqData, wantReqData)
	}
}
