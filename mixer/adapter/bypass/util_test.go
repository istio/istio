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

package bypass

import (
	"reflect"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"

	"istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/adapter"
)

func Test_convertValue(t *testing.T) {
	type args struct {
		value interface{}
	}
	tests := []struct {
		name    string
		args    args
		want    *v1beta1.Value
		wantErr bool
	}{
		{"string type", args{"foo"}, &v1beta1.Value{
			Value: &v1beta1.Value_StringValue{StringValue: "foo"},
		}, false},
		{"int64 type", args{int64(1)}, &v1beta1.Value{
			Value: &v1beta1.Value_Int64Value{Int64Value: int64(1)},
		}, false},
		{"float64 type", args{float64(1.0)}, &v1beta1.Value{
			Value: &v1beta1.Value_DoubleValue{DoubleValue: float64(1.0)},
		}, false},
		{"bool type", args{true}, &v1beta1.Value{
			Value: &v1beta1.Value_BoolValue{BoolValue: true},
		}, false},
		{"time duration type", args{1 * time.Second}, &v1beta1.Value{
			Value: &v1beta1.Value_DurationValue{DurationValue: &v1beta1.Duration{Value: types.DurationProto(1 * time.Second)}},
		}, false},
		{"time type", args{time.Time{}}, &v1beta1.Value{
			Value: &v1beta1.Value_TimestampValue{TimestampValue: &v1beta1.TimeStamp{Value: &types.Timestamp{Seconds: time.Time{}.Unix()}}},
		}, false},
		{"dnsname type", args{adapter.DNSName("dns")}, &v1beta1.Value{
			Value: &v1beta1.Value_DnsNameValue{DnsNameValue: &v1beta1.DNSName{Value: "dns"}},
		}, false},
		{"email address type", args{adapter.EmailAddress("istio@istio.io")}, &v1beta1.Value{
			Value: &v1beta1.Value_EmailAddressValue{EmailAddressValue: &v1beta1.EmailAddress{Value: "istio@istio.io"}},
		}, false},
		{"uri type", args{adapter.URI("https://istio.io")}, &v1beta1.Value{
			Value: &v1beta1.Value_UriValue{UriValue: &v1beta1.Uri{Value: "https://istio.io"}},
		}, false},
		{"byte type", args{[]byte("1.1.1.1")}, &v1beta1.Value{
			Value: &v1beta1.Value_IpAddressValue{IpAddressValue: &v1beta1.IPAddress{Value: []byte("1.1.1.1")}},
		}, false},
		{"invalid type", args{struct{}{}}, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertValue(tt.args.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_convertMapValue(t *testing.T) {
	type args struct {
		value map[string]interface{}
	}
	tests := []struct {
		name    string
		args    args
		want    map[string]*v1beta1.Value
		wantErr bool
	}{
		{
			"map value convert successful",
			args{map[string]interface{}{
				"foo": "bar",
			}},
			map[string]*v1beta1.Value{
				"foo": {
					Value: &v1beta1.Value_StringValue{StringValue: "bar"},
				},
			},
			false,
		},
		{
			"map value convert error",
			args{map[string]interface{}{
				"foo": struct{}{},
			}},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := convertMapValue(tt.args.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertMapValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertMapValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_newDedupID(t *testing.T) {
	id1 := newDedupID()
	id2 := newDedupID()
	if id1 == id2 {
		t.Errorf("newDedupID returns same id: %s", id1)
	}

	if len(id1) == 0 || len(id2) == 0 {
		t.Errorf("newDedupID returns empty id")
	}
}
