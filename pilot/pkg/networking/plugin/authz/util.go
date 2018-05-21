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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	policyproto "github.com/envoyproxy/go-control-plane/envoy/config/rbac/v2alpha"
	"github.com/envoyproxy/go-control-plane/envoy/type"
	"github.com/gogo/protobuf/types"
)

const (
	sourceIP        = "source.ip"
	sourceService   = "source.service"
	requestHeader   = "request.header"
	destinationIP   = "destination.ip"
	destinationPort = "destination.port"
)

func convertToPermissionCondition(k string, v []string) (*policyproto.Permission_Condition, error) {
	switch k {
	case destinationPort:
		if ports, err := convertToPortMatch(v); err != nil {
			return nil, err
		} else {
			return &policyproto.Permission_Condition{
				ConditionSpec: &policyproto.Permission_Condition_DestinationPorts{DestinationPorts: ports},
			}, nil
		}
	case destinationIP:
		if ips, err := convertToIPMatch(v); err != nil {
			return nil, err
		} else {
			return &policyproto.Permission_Condition{
				ConditionSpec: &policyproto.Permission_Condition_DestinationIps{DestinationIps: ips},
			}, nil
		}
	default:
		if header, err := convertToMapEntryMatch(requestHeader, k, v); err != nil {
			return nil, err
		} else {
			return &policyproto.Permission_Condition{
				ConditionSpec: &policyproto.Permission_Condition_Header{Header: header},
			}, nil
		}
	}
}

func convertToPrincipalAttribute(k, v string) (*policyproto.Principal_Attribute, error) {
	switch k {
	case sourceService:
		if v == "" {
			return nil, fmt.Errorf("empty service name")
		} else {
			return &policyproto.Principal_Attribute{
				AttributeSpec: &policyproto.Principal_Attribute_Service{Service: v},
			}, nil
		}
	case sourceIP:
		if ips, err := convertToIPMatch(strings.Split(v, ",")); err != nil {
			return nil, err
		} else {
			return &policyproto.Principal_Attribute{
				AttributeSpec: &policyproto.Principal_Attribute_SourceIps{SourceIps: ips},
			}, nil
		}
	default:
		// Do not split the value for header as we only supports a single value each header.
		if header, err := convertToMapEntryMatch(requestHeader, k, []string{v}); err != nil {
			return nil, err
		} else {
			return &policyproto.Principal_Attribute{
				AttributeSpec: &policyproto.Principal_Attribute_Header{Header: header},
			}, nil
		}
	}
}

// convertToStringMatch converts a string to a StringMatch, it supports four types of conversion:
// 1. Wild caracter match. i.e. "*" is converted to a regular expression match of "*"
// 2. Suffix match, e.g. "*abc" is converted to a suffix match of "abc"
// 3. Prefix match, e.g. "abc* " is converted to a prefix match of "abc"
// 4. Exact match, e.g. "abc" is converted to a simple exact match of "abc"
func convertToStringMatch(s string) *envoy_type.StringMatch {
	s = strings.TrimSpace(s)
	switch {
	case s == "*":
		return &envoy_type.StringMatch{MatchPattern: &envoy_type.StringMatch_Regex{Regex: "*"}}
	case strings.HasPrefix(s, "*"):
		return &envoy_type.StringMatch{MatchPattern: &envoy_type.StringMatch_Suffix{Suffix: strings.TrimPrefix(s, "*")}}
	case strings.HasSuffix(s, "*"):
		return &envoy_type.StringMatch{MatchPattern: &envoy_type.StringMatch_Prefix{Prefix: strings.TrimSuffix(s, "*")}}
	default:
		return &envoy_type.StringMatch{MatchPattern: &envoy_type.StringMatch_Simple{Simple: s}}
	}
}

// stringMatch checks if a string is in a list, it supports four types of string matches:
// 1. Exact match.
// 2. Wild character match. "*" matches any string.
// 3. Prefix match. For example, "book*" matches "bookstore", "bookshop", etc.
// 4. Suffix match. For example, "*/review" matches "/bookstore/review", "/products/review", etc.
func stringMatch(a string, list []string) bool {
	for _, s := range list {
		if a == s || s == "*" || prefixMatch(a, s) || suffixMatch(a, s) {
			return true
		}
	}
	return false
}

// prefixMatch checks if string "a" prefix matches "pattern".
func prefixMatch(a string, pattern string) bool {
	if !strings.HasSuffix(pattern, "*") {
		return false
	}
	pattern = strings.TrimSuffix(pattern, "*")
	return strings.HasPrefix(a, pattern)
}

// suffixMatch checks if string "a" prefix matches "pattern".
func suffixMatch(a string, pattern string) bool {
	if !strings.HasPrefix(pattern, "*") {
		return false
	}
	pattern = strings.TrimPrefix(pattern, "*")
	return strings.HasSuffix(a, pattern)
}

func convertToPortMatch(v []string) (*policyproto.PortMatch, error) {
	portMatch := &policyproto.PortMatch{}
	for _, port := range v {
		p, err := strconv.ParseUint(port, 10, 32)
		if err != nil || p < 0 {
			return nil, fmt.Errorf("invalid port %s: %v", port, err)
		}
		portMatch.Ports = append(portMatch.Ports, uint32(p))
	}
	return portMatch, nil
}

func convertToIPMatch(v []string) (*policyproto.IpMatch, error) {
	ipMatch := &policyproto.IpMatch{}
	for _, cidr := range v {
		splits := strings.Split(cidr, "/")
		if len(splits) != 2 {
			return nil, fmt.Errorf("invalid CIDR range: %s", cidr)
		}

		prefixLen, err := strconv.ParseUint(splits[1], 10, 32)
		if err != nil || prefixLen < 0 {
			return nil, fmt.Errorf("invalid prefix length in CIDR range: %s: %v", splits[1], err)
		}

		ipMatch.Cidrs = append(ipMatch.Cidrs, &core.CidrRange{
			AddressPrefix: splits[0],
			PrefixLen:     &types.UInt32Value{Value: uint32(prefixLen)},
		})
	}
	return ipMatch, nil
}

func convertToMapEntryMatch(prefix, k string, v []string) (*policyproto.MapEntryMatch, error) {
	// Regular expression to match the key that is made from a prefix and header name in the format
	// prefix[HEADER_NAME], e.g. request.header[USER-ID].
	keyRegex := fmt.Sprintf(`^%s\[(\S+)\]$`, prefix)
	headers := regexp.MustCompile(keyRegex).FindStringSubmatch(k)
	// headers[1] contains the actual extracted header name, e.g. USER-ID.
	if len(headers) != 2 || headers[1] == "" {
		return nil, fmt.Errorf("invalid header format, the format should be %s[KeyName]", prefix)
	}

	mapEntryMatch := &policyproto.MapEntryMatch{Key: headers[1]}
	for _, s := range v {
		mapEntryMatch.Values = append(mapEntryMatch.Values, convertToStringMatch(s))
	}
	return mapEntryMatch, nil
}
