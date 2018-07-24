// Copyright 2018 Istio Authors
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

package svctype

import (
	"encoding/json"
	"testing"
)

func TestFromString(t *testing.T) {
	tests := []struct {
		input       string
		serviceType ServiceType
		err         error
	}{
		{"http", ServiceHTTP, nil},
		{"grpc", ServiceGRPC, nil},
		{"", ServiceUnknown, InvalidServiceTypeStringError{""}},
		{"cat", ServiceUnknown, InvalidServiceTypeStringError{"cat"}},
	}

	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			t.Parallel()

			serviceType, err := FromString(test.input)
			if test.err != err {
				t.Errorf("expected %v; actual %v", test.err, err)
			}
			if test.serviceType != serviceType {
				t.Errorf("expected %v; actual %v", test.serviceType, serviceType)
			}
		})
	}
}

func TestServiceType_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input       []byte
		serviceType ServiceType
		err         error
	}{
		{[]byte(`"http"`), ServiceHTTP, nil},
		{[]byte(`"grpc"`), ServiceGRPC, nil},
		{[]byte(`""`), ServiceUnknown, InvalidServiceTypeStringError{""}},
		{[]byte(`"cat"`), ServiceUnknown, InvalidServiceTypeStringError{"cat"}},
	}

	for _, test := range tests {
		test := test
		t.Run("", func(t *testing.T) {
			t.Parallel()

			var serviceType ServiceType
			err := json.Unmarshal(test.input, &serviceType)
			if test.err != err {
				t.Errorf("expected %v; actual %v", test.err, err)
			}
			if test.serviceType != serviceType {
				t.Errorf("expected %v; actual %v", test.serviceType, serviceType)
			}
		})
	}
}
