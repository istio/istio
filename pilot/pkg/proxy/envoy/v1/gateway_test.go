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

package v1

import (
	"reflect"
	"testing"

	"github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func TestFindRulesAndMatchingHosts(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	rule1 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name: "some-rule",
		},
		Spec: &networking.VirtualService{
			Hosts: []string{
				"foo.com",
				"bar.com",
			},
			Gateways: []string{
				"our-gateway",
			},
		},
	}
	rule2 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name: "some-other-rule",
		},
		Spec: &networking.VirtualService{
			Hosts: []string{
				"other-foo.com",
				"other-foo.org",
				"help.other-bar.com",
			},
			Gateways: []string{
				"some-other-gateway",
			},
		},
	}
	rule3 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name: "our-rule",
		},
		Spec: &networking.VirtualService{
			Hosts: []string{
				"example.com",
			},
			Gateways: []string{
				"our-gateway",
			},
		},
	}

	allRules := []model.Config{
		rule1,
		rule2,
		rule3,
	}

	testCases := []struct {
		gatewayName    string
		gatewayHost    string
		expectedResult []ruleWithHosts
	}{
		{
			gatewayName: "our-gateway",
			gatewayHost: "example.com",
			expectedResult: []ruleWithHosts{
				{
					Rule:  rule3,
					Hosts: []string{"example.com"},
				},
			},
		},
		{
			gatewayName: "some-other-gateway",
			gatewayHost: "*.com",
			expectedResult: []ruleWithHosts{
				{
					Rule:  rule2,
					Hosts: []string{"other-foo.com", "help.other-bar.com"},
				},
			},
		},
	}

	for _, testCase := range testCases {
		actualResult := findRulesAndMatchingHosts(allRules, testCase.gatewayName, testCase.gatewayHost)
		g.Expect(actualResult).To(gomega.Equal(testCase.expectedResult))
	}
}

func TestFindMatchingHosts(t *testing.T) {
	hosts := []string{"help.example.com",
		"help.example.org",
		"example.com",
		"foo.example.com",
		"afoo.example.com",
		".example.com",
		"find.example.com"}

	cases := []struct {
		matchCriteria         string
		expectedMatchingHosts []string
	}{
		{
			matchCriteria: "*.example.com",
			expectedMatchingHosts: []string{
				"help.example.com",
				"foo.example.com",
				"afoo.example.com",
				".example.com",
				"find.example.com",
			},
		},
		{
			matchCriteria: "foo.example.com",
			expectedMatchingHosts: []string{
				"foo.example.com",
			},
		},
		{
			matchCriteria:         "",
			expectedMatchingHosts: nil,
		},
	}

	for _, testCase := range cases {
		matchingHosts := findMatchingHosts(testCase.matchCriteria, hosts)

		ok := reflect.DeepEqual(testCase.expectedMatchingHosts, matchingHosts)
		if !ok {
			t.Errorf("got matches %+v, wanted %+v", matchingHosts, testCase.expectedMatchingHosts)
		}
	}
}
