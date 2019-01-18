//  Copyright 2019 Istio Authors
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

package conversions

import (
	"istio.io/istio/galley/pkg/runtime/resource"
	"reflect"
	"sort"
	"testing"
)

func TestAuthConverter(t *testing.T) {
	ac := AuthConverter{}

	services := []struct {
		Name      string
		Namespace string
		Selector  resource.Labels
	}{
		{Name: "svc1", Namespace: "ns1", Selector: resource.Labels{"version": "1"}},
		{Name: "svc2", Namespace: "ns1", Selector: resource.Labels{"version": "2"}},
		{Name: "headless", Namespace: "ns1"},
		{Name: "svc4", Namespace: "ns2", Selector: resource.Labels{"version": "4"}},
		{Name: "svc5", Namespace: "ns2", Selector: resource.Labels{"version": "5"}},
		{Name: "foo.svc", Namespace: "ns3", Selector: resource.Labels{"version": "6"}},
		{Name: "bar.svc", Namespace: "ns3", Selector: resource.Labels{"version": "7"}},
		{Name: "svc.foo", Namespace: "ns3", Selector: resource.Labels{"version": "8"}},
		{Name: "svc.bar", Namespace: "ns3", Selector: resource.Labels{"version": "9"}},
	}

	for _, svc := range services {
		ac.AddService(resource.FullNameFromNamespaceAndName(svc.Namespace, svc.Name), svc.Selector)
	}

	testCases := []struct {
		Name                  string
		Namespace             string
		AllowPrefixSuffixName bool
		ExpectSelectors       []resource.Labels
	}{
		{
			Name:                  "non-exist-svc",
			Namespace:             "ns1",
			AllowPrefixSuffixName: false,
			ExpectSelectors:       make([]resource.Labels, 0),
		},
		{
			Name:                  "svc1",
			Namespace:             "non-exist-namespace",
			AllowPrefixSuffixName: false,
			ExpectSelectors:       make([]resource.Labels, 0),
		},
		{
			Name:                  "svc1",
			Namespace:             "ns1",
			AllowPrefixSuffixName: false,
			ExpectSelectors:       []resource.Labels{{"version": "1"}},
		},
		{
			Name:                  "svc2",
			Namespace:             "ns1",
			AllowPrefixSuffixName: false,
			ExpectSelectors:       []resource.Labels{{"version": "2"}},
		},
		{
			Name:                  "headless",
			Namespace:             "ns1",
			AllowPrefixSuffixName: false,
			ExpectSelectors:       make([]resource.Labels, 0),
		},
		{
			Name:                  "svc4",
			Namespace:             "ns2",
			AllowPrefixSuffixName: true,
			ExpectSelectors:       []resource.Labels{{"version": "4"}},
		},
		{
			Name:                  "svc5",
			Namespace:             "ns2",
			AllowPrefixSuffixName: true,
			ExpectSelectors:       []resource.Labels{{"version": "5"}},
		},
		{
			Name:                  "*",
			Namespace:             "ns3",
			AllowPrefixSuffixName: true,
			ExpectSelectors: []resource.Labels{{"version": "6"}, {"version": "7"},
				{"version": "8"}, {"version": "9"}},
		},
		{
			Name:                  "*.svc",
			Namespace:             "ns3",
			AllowPrefixSuffixName: true,
			ExpectSelectors:       []resource.Labels{{"version": "6"}, {"version": "7"}},
		},
		{
			Name:                  "svc.*",
			Namespace:             "ns3",
			AllowPrefixSuffixName: true,
			ExpectSelectors:       []resource.Labels{{"version": "8"}, {"version": "9"}},
		},
		{
			Name:                  "*.*",
			Namespace:             "ns3",
			AllowPrefixSuffixName: true,
			ExpectSelectors:       make([]resource.Labels, 0),
		},
	}

	for _, tc := range testCases {
		actual := ac.GetSelectors(tc.Name, tc.Namespace, tc.AllowPrefixSuffixName)
		// Sort the result to make the test stable.
		sort.Slice(actual, func(i, j int) bool {
			return actual[i]["version"] < actual[j]["version"]
		})
		if !reflect.DeepEqual(actual, tc.ExpectSelectors) {
			t.Errorf("failed for %v, expect: %v but got %v", tc, tc.ExpectSelectors, actual)
		}
	}
}
