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
