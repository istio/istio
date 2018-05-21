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

package authz

import (
	"reflect"
	"strings"
	"testing"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	policyproto "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	"github.com/envoyproxy/go-control-plane/envoy/type"
	"github.com/gogo/protobuf/types"
)

func TestConvertToPermissionCondition(t *testing.T) {
	testCases := []struct {
		Name      string
		K         string
		V         []string
		Condition *policyproto.Permission_Condition
		Err       string
	}{
		{
			Name: "destination.port", K: "destination.port", V: []string{"80", "443"},
			Condition: &policyproto.Permission_Condition{
				ConditionSpec: &policyproto.Permission_Condition_DestinationPorts{
					DestinationPorts: &policyproto.PortMatch{Ports: []uint32{80, 443}},
				},
			},
		},
		{
			Name: "invalid destination.port", K: "destination.port", V: []string{"80", "xyz"},
			Err: "invalid port xyz",
		},
		{
			Name: "destination.ip", K: "destination.ip", V: []string{"192.1.2.0/24", "2001:db8::/28"},
			Condition: &policyproto.Permission_Condition{
				ConditionSpec: &policyproto.Permission_Condition_DestinationIps{
					DestinationIps: &policyproto.IpMatch{Cidrs: []*core.CidrRange{
						{AddressPrefix: "192.1.2.0", PrefixLen: &types.UInt32Value{Value: 24}},
						{AddressPrefix: "2001:db8::", PrefixLen: &types.UInt32Value{Value: 28}},
					}},
				},
			},
		},
		{
			Name: "invalid destination.ip", K: "destination.ip", V: []string{"192.1.2.0/24", "2001:db8::28"},
			Err: "invalid CIDR range: 2001:db8::28",
		},
		{
			Name: "request.header", K: "request.header[USER-ID]", V: []string{"prefix*", "simple"},
			Condition: &policyproto.Permission_Condition{
				ConditionSpec: &policyproto.Permission_Condition_Header{
					Header: &policyproto.MapEntryMatch{
						Key: "USER-ID",
						Values: []*envoy_type.StringMatch{
							{MatchPattern: &envoy_type.StringMatch_Prefix{Prefix: "prefix"}},
							{MatchPattern: &envoy_type.StringMatch_Simple{Simple: "simple"}},
						},
					},
				},
			},
		},
		{
			Name: "invalid request.header", K: "request.header{USER-ID}", V: []string{"prefix*", "simple"},
			Err: "invalid header format, the format should be request.header[KeyName]",
		},
	}

	for _, tc := range testCases {
		actual, err := convertToPermissionCondition(tc.K, tc.V)
		if tc.Err != "" {
			if err == nil {
				t.Errorf("%s: expecting error: %s, but got no error", tc.Name, tc.Err)
			} else if !strings.HasPrefix(err.Error(), tc.Err) {
				t.Errorf("%s: expecting error: %s, but got %s", tc.Name, tc.Err, err.Error())
			}
		} else if !reflect.DeepEqual(tc.Condition, actual) {
			t.Errorf("%s: expecting condition: %v, but got %v",
				tc.Name, tc.Condition.ConditionSpec, actual.ConditionSpec)
		}
	}
}

func TestConvertToPrincipalAttribute(t *testing.T) {
	testCases := []struct {
		Name      string
		K         string
		V         string
		Principal *policyproto.Principal_Attribute
		Err       string
	}{
		{
			Name: "source.service", K: "source.service", V: "productpage",
			Principal: &policyproto.Principal_Attribute{
				AttributeSpec: &policyproto.Principal_Attribute_Service{
					Service: "productpage",
				},
			},
		},
		{
			Name: "invalid source.service", K: "source.service", V: "",
			Err: "empty service name",
		},
		{
			Name: "source.ip", K: "source.ip", V: "192.1.2.0/24,2001:db8::/28",
			Principal: &policyproto.Principal_Attribute{
				AttributeSpec: &policyproto.Principal_Attribute_SourceIps{
					SourceIps: &policyproto.IpMatch{
						Cidrs: []*core.CidrRange{
							{AddressPrefix: "192.1.2.0", PrefixLen: &types.UInt32Value{Value: 24}},
							{AddressPrefix: "2001:db8::", PrefixLen: &types.UInt32Value{Value: 28}},
						},
					},
				},
			},
		},
		{
			Name: "invalid source.ip", K: "source.ip", V: "192.1.2.0/24;2001:db8::/28",
			Err: "invalid CIDR range",
		},
		{
			Name: "request.header", K: "request.header[USER-ID]", V: "prefix*",
			Principal: &policyproto.Principal_Attribute{
				AttributeSpec: &policyproto.Principal_Attribute_Header{
					Header: &policyproto.MapEntryMatch{
						Key: "USER-ID",
						Values: []*envoy_type.StringMatch{
							{MatchPattern: &envoy_type.StringMatch_Prefix{Prefix: "prefix"}},
						},
					},
				},
			},
		},
		{
			Name: "invalid request.header", K: "request.header{USER-ID}", V: "prefix*",
			Err: "invalid header format, the format should be request.header[KeyName]",
		},
	}

	for _, tc := range testCases {
		actual, err := convertToPrincipalAttribute(tc.K, tc.V)
		if tc.Err != "" {
			if err == nil {
				t.Errorf("%s: expecting error: %s, but got no error", tc.Name, tc.Err)
			} else if !strings.HasPrefix(err.Error(), tc.Err) {
				t.Errorf("%s: expecting error: %s, but got: %s", tc.Name, tc.Err, err.Error())
			}
		} else if !reflect.DeepEqual(tc.Principal, actual) {
			t.Errorf("%s: expecting condition: %v, but got: %v",
				tc.Name, tc.Principal.AttributeSpec, actual.AttributeSpec)
		}
	}
}

func TestConvertToStringMatch(t *testing.T) {
	testCases := []struct {
		Name   string
		S      string
		Expect *envoy_type.StringMatch
	}{
		{
			Name: "exact match", S: "	product page ",
			Expect: &envoy_type.StringMatch{
				MatchPattern: &envoy_type.StringMatch_Simple{Simple: "product page"},
			},
		},
		{
			Name: "wild character match", S: " * ",
			Expect: &envoy_type.StringMatch{
				MatchPattern: &envoy_type.StringMatch_Regex{Regex: "*"},
			},
		},
		{
			Name: "prefix match", S: " product page* ",
			Expect: &envoy_type.StringMatch{
				MatchPattern: &envoy_type.StringMatch_Prefix{Prefix: "product page"},
			},
		},
		{
			Name: "suffix match", S: " *product page ",
			Expect: &envoy_type.StringMatch{
				MatchPattern: &envoy_type.StringMatch_Suffix{Suffix: "product page"},
			},
		},
	}

	for _, tc := range testCases {
		if actual := convertToStringMatch(tc.S); !reflect.DeepEqual(actual, tc.Expect) {
			t.Errorf("%s: expecting: %v, but got: %v", tc.Name, tc.Expect.MatchPattern, actual.MatchPattern)
		}
	}
}

func TestStringMatch(t *testing.T) {
	testCases := []struct {
		Name   string
		S      string
		List   []string
		Expect bool
	}{
		{
			Name: "exact match", S: "product page", List: []string{"review page", "product page"},
			Expect: true,
		},
		{
			Name: "wild character match", S: "product page", List: []string{"review page", "*"},
			Expect: true,
		},
		{
			Name: "prefix match", S: "product page", List: []string{"review page", "product*"},
			Expect: true,
		},
		{
			Name: "suffix match", S: "product page", List: []string{"review page", "*page"},
			Expect: true,
		},
		{
			Name: "not matched", S: "product page", List: []string{"review page", "xyz product page"},
			Expect: false,
		},
	}

	for _, tc := range testCases {
		if actual := stringMatch(tc.S, tc.List); actual != tc.Expect {
			t.Errorf("%s: expecting: %v, but got: %v", tc.Name, tc.Expect, actual)
		}
	}
}
