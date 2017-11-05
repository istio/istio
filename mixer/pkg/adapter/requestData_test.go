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

package adapter

import (
	"context"
	"reflect"
	"testing"
)

func TestRequestDataFromContext(t *testing.T) {
	wantReqData := &RequestData{DestinationService: Service{FullName: "foo.bar"}}
	ctx := context.WithValue(context.Background(), requestDataKey, wantReqData)
	got, gotOk := RequestDataFromContext(ctx)
	if !gotOk || got.DestinationService.FullName != "foo.bar" {
		t.Errorf("RequestDataFromContext(%v) = (%v,%v), want (%v,%v)", ctx, *got, gotOk, *wantReqData, true)
	}
}

func TestRequestDataFromContext_NotPresent(t *testing.T) {
	ctx := context.Background()
	got, gotOk := RequestDataFromContext(context.Background())
	if gotOk {
		t.Errorf("RequestDataFromContext(%v) = (%v,%v), want (%v,%v)", ctx, got, gotOk, nil, false)
	}
}

func TestNewContextWithRequestData(t *testing.T) {
	wantReqData := &RequestData{DestinationService: Service{FullName: "foo.bar"}}
	ctx := NewContextWithRequestData(context.Background(), wantReqData)
	got := ctx.Value(requestDataKey).(*RequestData)
	if !reflect.DeepEqual(got, wantReqData) {
		t.Errorf("NewContextWithRequestData added RequestData = %v, want %v", *got, *wantReqData)
	}
}
