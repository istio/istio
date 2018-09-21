package spiffe

import (
	"strings"
	"testing"
)

func TestGenSpiffeURI(t *testing.T) {
	testCases := []struct {
		namespace      string
		serviceAccount string
		expectedError  string
		expectedURI    string
	}{
		{
			serviceAccount: "sa",
			expectedError:  "namespace or service account can't be empty",
		},
		{
			namespace:     "ns",
			expectedError: "namespace or service account can't be empty",
		},
		{
			namespace:      "namespace-foo",
			serviceAccount: "service-bar",
			expectedURI:    "spiffe://cluster.local/ns/namespace-foo/sa/service-bar",
		},
	}
	for id, tc := range testCases {
		got, err := GenSpiffeURI(tc.namespace, tc.serviceAccount)
		if tc.expectedError == "" && err != nil {
			t.Errorf("teste case [%v] failed, error %v", id, tc)
		}
		if tc.expectedError != "" {
			if err == nil {
				t.Errorf("want get error %v, got nil", tc.expectedError)
			} else if !strings.Contains(err.Error(), tc.expectedError) {
				t.Errorf("want error contains %v,  got error %v", tc.expectedError, err)
			}
			continue
		}
		if got != tc.expectedURI {
			t.Errorf("unexpected subject name, want %v, got %v", tc.expectedURI, got)
		}

	}
}

func TestGetSetTrustDomain(t *testing.T) {
	oldTrustDomain := GetIdentityDomain()
	defer SetIdentityDomain(oldTrustDomain)
	SetIdentityDomain("test.local")
	if GetIdentityDomain() != "test.local" {
		t.Errorf("Set/GetIdentityDomain not working")
	}
}

func TestMustGenSpiffeURI(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expect that MustGenSpiffeURI panics in case of empty namespace")
		}
	}()

	MustGenSpiffeURI("", "")
}
