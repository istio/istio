package spiffe

import (
	"testing"
)

func TestGenSpiffeURI(t *testing.T) {
	oldTrustDomain := GetTrustDomain()
	defer SetTrustDomain(oldTrustDomain)

	testCases := []struct {
		namespace      string
		trustDomain    string
		serviceAccount string
		expectedURI    string
	}{
		{
			serviceAccount: "sa",
			expectedURI:    "spiffe://cluster.local/ns//sa/sa",
		},
		{
			namespace:   "ns",
			expectedURI: "spiffe://cluster.local/ns/ns/sa/",
		},
		{
			namespace:      "namespace-foo",
			serviceAccount: "service-bar",
			expectedURI:    "spiffe://cluster.local/ns/namespace-foo/sa/service-bar",
		},
		{
			namespace:      "foo",
			serviceAccount: "bar",
			expectedURI:    "spiffe://cluster.local/ns/foo/sa/bar",
		},
		{
			namespace:      "foo",
			serviceAccount: "bar",
			trustDomain:    "kube-federating-id@testproj.iam.gserviceaccount.com",
			expectedURI:    "spiffe://kube-federating-id.testproj.iam.gserviceaccount.com/ns/foo/sa/bar",
		},
	}

	for _, tc := range testCases {
		if tc.trustDomain == "" {
			SetTrustDomain(defaultTrustDomain)
		} else {
			SetTrustDomain(tc.trustDomain)
		}

		got := GenSpiffeURI(tc.namespace, tc.serviceAccount)
		if got != tc.expectedURI {
			t.Errorf("unexpected subject name, want %v, got %v", tc.expectedURI, got)
		}
	}
}

func TestGetSetTrustDomain(t *testing.T) {
	oldTrustDomain := GetTrustDomain()
	defer SetTrustDomain(oldTrustDomain)
	SetTrustDomain("test.local")
	if GetTrustDomain() != "test.local" {
		t.Errorf("Set/GetTrustDomain not working")
	}
}

func TestMustGenSpiffeURI(t *testing.T) {
	if nonsense := GenSpiffeURI("", ""); nonsense != "spiffe://cluster.local/ns//sa/" {
		t.Errorf("Unexpected spiffe URI for empty namespace and service account: %s", nonsense)
	}
}
