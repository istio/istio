// Copyright 2019 Istio Authors
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

package controller

import (
	"strings"
	"testing"
)

func TestConstructCustomDNSNames(t *testing.T) {
	testCases := map[string]struct {
		sas      []string
		sns      []string
		ns       string
		dnsNames string
		expected map[string]*DNSNameEntry
	}{
		"Valid input 1": {
			sas: []string{
				"istio-sidecar-injector-service-account",
			},
			sns: []string{
				"istio-sidecar-injector",
			},
			ns: "istio-system",
			expected: map[string]*DNSNameEntry{
				"istio-sidecar-injector-service-account": {
					ServiceName: "istio-sidecar-injector",
					Namespace:   "istio-system",
				},
			},
		},
		"Valid input 2": {
			sas: []string{
				"istio-sidecar-injector-service-account",
				"istio-galley-service-account",
			},
			sns: []string{
				"istio-sidecar-injector",
				"istio-galley",
			},
			ns: "istio-system",
			expected: map[string]*DNSNameEntry{
				"istio-sidecar-injector-service-account": {
					ServiceName: "istio-sidecar-injector",
					Namespace:   "istio-system",
				},
				"istio-galley-service-account": {
					ServiceName: "istio-galley",
					Namespace:   "istio-system",
				},
			},
		},
		"Valid input 3": {
			sas: []string{
				"istio-sidecar-injector-service-account",
				"istio-galley-service-account",
			},
			sns: []string{
				"istio-sidecar-injector",
				"istio-galley",
			},
			ns:       "istio-system",
			dnsNames: "istio-galley-service-account.istio-system:istio-galley-ilb.istio-system.svc.us1.dog",
			expected: map[string]*DNSNameEntry{
				"istio-sidecar-injector-service-account": {
					ServiceName: "istio-sidecar-injector",
					Namespace:   "istio-system",
				},
				"istio-galley-service-account": {
					ServiceName: "istio-galley",
					Namespace:   "istio-system",
				},
				"istio-galley-service-account.istio-system": {
					ServiceName:   "istio-galley-service-account.istio-system",
					CustomDomains: []string{"istio-galley-ilb.istio-system.svc.us1.dog"},
				},
			},
		},
		"Valid input 4": {
			sas: []string{
				"istio-sidecar-injector-service-account",
				"istio-galley-service-account",
			},
			sns: []string{
				"istio-sidecar-injector",
				"istio-galley",
			},
			ns: "istio-system",
			dnsNames: "istio-galley-service-account.istio-system:istio-galley-ilb.istio-system.svc.us1," +
				"istio-galley-service-account.istio-system:istio-galley-ilb.istio-system.svc.us2",
			expected: map[string]*DNSNameEntry{
				"istio-sidecar-injector-service-account": {
					ServiceName: "istio-sidecar-injector",
					Namespace:   "istio-system",
				},
				"istio-galley-service-account": {
					ServiceName: "istio-galley",
					Namespace:   "istio-system",
				},
				"istio-galley-service-account.istio-system": {
					ServiceName:   "istio-galley-service-account.istio-system",
					CustomDomains: []string{"istio-galley-ilb.istio-system.svc.us1", "istio-galley-ilb.istio-system.svc.us2"},
				},
			},
		},
		"Invalid input 1": {
			sas: []string{
				"istio-sidecar-injector-service-account",
				"istio-galley-service-account",
			},
			sns: []string{
				"istio-sidecar-injector",
				"istio-galley",
			},
			ns: "istio-system",
			dnsNames: "istio-galley-service-account.istio-systemistio-galley-ilb.istio-system.svc.us1," +
				"istio-galley-service-account.istio-system:istio-galley-ilb.istio-system.svc.us2",
			expected: map[string]*DNSNameEntry{
				"istio-sidecar-injector-service-account": {
					ServiceName: "istio-sidecar-injector",
					Namespace:   "istio-system",
				},
				"istio-galley-service-account": {
					ServiceName: "istio-galley",
					Namespace:   "istio-system",
				},
				"istio-galley-service-account.istio-system": {
					ServiceName:   "istio-galley-service-account.istio-system",
					CustomDomains: []string{"istio-galley-ilb.istio-system.svc.us2"},
				},
			},
		},
		"Invalid input 2": {
			sas: []string{
				"istio-sidecar-injector-service-account",
				"istio-galley-service-account",
			},
			sns: []string{
				"istio-sidecar-injector",
				"istio-galley",
			},
			ns:       "istio-system",
			dnsNames: "istio-galley-service-account.istiley-ilb.is,istio-galley-service-account.istio-sysgalley-ilb.istio-system.svc.us2",
			expected: map[string]*DNSNameEntry{
				"istio-sidecar-injector-service-account": {
					ServiceName: "istio-sidecar-injector",
					Namespace:   "istio-system",
				},
				"istio-galley-service-account": {
					ServiceName: "istio-galley",
					Namespace:   "istio-system",
				},
			},
		},
		"Valid input 5": {
			sas: []string{
				"istio-sidecar-injector-service-account",
				"istio-galley-service-account",
			},
			sns: []string{
				"istio-sidecar-injector",
				"istio-galley",
			},
			ns:       "istio-system",
			dnsNames: "istio-mixer.istio-system:istio-mixer.istio-system.us1,istio-galley-service-account.istio-system:istio-galley-ilb.istio-system.svc.us2",
			expected: map[string]*DNSNameEntry{
				"istio-sidecar-injector-service-account": {
					ServiceName: "istio-sidecar-injector",
					Namespace:   "istio-system",
				},
				"istio-galley-service-account": {
					ServiceName: "istio-galley",
					Namespace:   "istio-system",
				},
				"istio-mixer.istio-system": {
					ServiceName:   "istio-mixer.istio-system",
					CustomDomains: []string{"istio-mixer.istio-system.us1"},
				},
				"istio-galley-service-account.istio-system": {
					ServiceName:   "istio-galley-service-account.istio-system",
					CustomDomains: []string{"istio-galley-ilb.istio-system.svc.us2"},
				},
			},
		},
	}

	for k, tc := range testCases {
		result := ConstructCustomDNSNames(tc.sas, tc.sns, tc.ns, tc.dnsNames)
		if len(result) != len(tc.expected) {
			t.Errorf("Test case %s: expected entry %v, actual %v", k, len(tc.expected), len(result))
		}
		for key, value := range tc.expected {
			compareDNSNameEntry(t, k, value, result[key])
		}
	}
}

func compareDNSNameEntry(t *testing.T, k string, expected *DNSNameEntry, result *DNSNameEntry) {
	if strings.Compare(expected.ServiceName, result.ServiceName) != 0 {
		t.Errorf("Failure in test case %s: expected servicename %v, result %v", k, expected.ServiceName, result.ServiceName)
	}
	if strings.Compare(expected.Namespace, result.Namespace) != 0 {
		t.Errorf("Failure in test case %s: expected namespace %v, result %v", k, expected.Namespace, result.Namespace)
	}
	if len(expected.CustomDomains) != len(result.CustomDomains) {
		t.Errorf("Failure in test case %s: expected customDomain length %d, result %d", k, len(expected.CustomDomains), len(result.CustomDomains))
	} else {
		for i, cd := range expected.CustomDomains {
			if strings.Compare(cd, result.CustomDomains[i]) != 0 {
				t.Errorf("Failure in test case %s: expected customDomain[%d] be %v, result %v", k, i, cd, result.CustomDomains[i])
			}
		}
	}
}
