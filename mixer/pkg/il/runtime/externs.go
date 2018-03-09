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

package runtime

import (
	"fmt"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/idna"
	config "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/expr"
	"istio.io/istio/mixer/pkg/il/interpreter"
)

// Externs contains the list of standard external functions used during evaluation.
var Externs = map[string]interpreter.Extern{
	"ip":              interpreter.ExternFromFn("ip", externIP),
	"ip_equal":        interpreter.ExternFromFn("ip_equal", externIPEqual),
	"timestamp":       interpreter.ExternFromFn("timestamp", externTimestamp),
	"timestamp_equal": interpreter.ExternFromFn("timestamp_equal", externTimestampEqual),
	"dnsName":         interpreter.ExternFromFn("dnsName", externDnsName),
	"dnsName_equal":   interpreter.ExternFromFn("dnsName_equal", externDnsNameEqual),
	"email":           interpreter.ExternFromFn("email", externEmail),
	"email_equal":     interpreter.ExternFromFn("email_equal", externEmailEqual),
	"match":           interpreter.ExternFromFn("match", externMatch),
	"matches":         interpreter.ExternFromFn("matches", externMatches),
	"startsWith":      interpreter.ExternFromFn("startsWith", externStartsWith),
	"endsWith":        interpreter.ExternFromFn("endsWith", externEndsWith),
	"emptyStringMap":  interpreter.ExternFromFn("emptyStringMap", externEmptyStringMap),
}

// ExternFunctionMetadata is the type-metadata about externs. It gets used during compilations.
var ExternFunctionMetadata = []expr.FunctionMetadata{
	{
		Name:          "ip",
		ReturnType:    config.IP_ADDRESS,
		ArgumentTypes: []config.ValueType{config.STRING},
	},
	{
		Name:          "timestamp",
		ReturnType:    config.TIMESTAMP,
		ArgumentTypes: []config.ValueType{config.STRING},
	},
	{
		Name:          "dnsName",
		ReturnType:    config.DNS_NAME,
		ArgumentTypes: []config.ValueType{config.STRING},
	},
	{
		Name:          "email",
		ReturnType:    config.EMAIL_ADDRESS,
		ArgumentTypes: []config.ValueType{config.STRING},
	},
	{
		Name:          "match",
		ReturnType:    config.BOOL,
		ArgumentTypes: []config.ValueType{config.STRING, config.STRING},
	},
	{
		Name:          "matches",
		Instance:      true,
		TargetType:    config.STRING,
		ReturnType:    config.BOOL,
		ArgumentTypes: []config.ValueType{config.STRING},
	},
	{
		Name:          "startsWith",
		Instance:      true,
		TargetType:    config.STRING,
		ReturnType:    config.BOOL,
		ArgumentTypes: []config.ValueType{config.STRING},
	},
	{
		Name:          "endsWith",
		Instance:      true,
		TargetType:    config.STRING,
		ReturnType:    config.BOOL,
		ArgumentTypes: []config.ValueType{config.STRING},
	},
	{
		Name:          "emptyStringMap",
		ReturnType:    config.STRING_MAP,
		ArgumentTypes: []config.ValueType{},
	},
}

func externIP(in string) ([]byte, error) {
	if ip := net.ParseIP(in); ip != nil {
		return []byte(ip), nil
	}
	return []byte{}, fmt.Errorf("could not convert %s to IP_ADDRESS", in)
}

func externIPEqual(a []byte, b []byte) bool {
	// net.IP is an alias for []byte, so these are safe to convert
	ip1 := net.IP(a)
	ip2 := net.IP(b)
	return ip1.Equal(ip2)
}

func externTimestamp(in string) (time.Time, error) {
	layout := time.RFC3339
	t, err := time.Parse(layout, in)
	if err != nil {
		return time.Time{}, fmt.Errorf("could not convert '%s' to TIMESTAMP. expected format: '%s'", in, layout)
	}
	return t, nil
}

func externTimestampEqual(t1 time.Time, t2 time.Time) bool {
	return t1.Equal(t2)
}

// This profile is for performing  validations, but does not otherwise modify the string.
var externDnsNameProfile = idna.New(
	idna.StrictDomainName(true),
	idna.ValidateLabels(true),
	idna.VerifyDNSLength(true),
	idna.BidiRule())

func externDnsName(in string) (string, error) {
	s, err := externDnsNameProfile.ToUnicode(in)
	if err != nil {
		err = fmt.Errorf("Error converting '%s' to dns name: '%v'", in, err)
	}
	return s, err
}

// This profile converts the string for lookup.
var externDnsNameEqualProfile = idna.New(idna.MapForLookup(),
	idna.BidiRule())

func externDnsNameEqual(n1 string, n2 string) (bool, error) {
	var err error

	if n1, err = externDnsNameEqualProfile.ToUnicode(n1); err != nil {
		return false, err
	}

	if n2, err = externDnsNameEqualProfile.ToUnicode(n2); err != nil {
		return false, err
	}

	if n1[len(n1)-1] == '.' && n2[len(n2)-1] != '.' {
		n1 = n1[:len(n1)-1]
	}
	if n2[len(n2)-1] == '.' && n1[len(n1)-1] != '.' {
		n2 = n2[:len(n2)-1]
	}

	return n1 == n2, nil
}

func externEmail(in string) (string, error) {
	a, err := mail.ParseAddress(in)
	if err != nil {
		return "", fmt.Errorf("error converting '%s' to e-mail: '%v'", in, err)
	}

	if a.Name != "" {
		return "", fmt.Errorf("error converting '%s' to e-mail: display names are not allowed", in)
	}

	// Also check through the dns name logic to ensure that this will not cause any breaks there, when used for
	// comparison.

	_, domain := getEmailParts(a.Address)

	_, err = externDnsName(domain)
	if err != nil {
		return "", fmt.Errorf("error converting '%s' to e-mail: '%v'", in, err)
	}

	return in, nil
}

func externEmailEqual(e1 string, e2 string) (bool, error) {
	a1, err := mail.ParseAddress(e1)
	if err != nil {
		return false, err
	}

	a2, err := mail.ParseAddress(e2)
	if err != nil {
		return false, err
	}

	local1, domain1 := getEmailParts(a1.Address)
	local2, domain2 := getEmailParts(a2.Address)

	domainEq, err := externDnsNameEqual(domain1, domain2)
	if err != nil {
		return false, fmt.Errorf("error comparing e-mails '%s' and '%s': %v", e1, e2, err)
	}

	if !domainEq {
		return false, nil
	}

	return local1 == local2, nil
}

func getEmailParts(email string) (local string, domain string) {
	idx := strings.IndexByte(email, '@')
	if idx == -1 {
		local = email
		domain = ""
		return
	}

	local = email[:idx]
	domain = email[idx+1:]
	return
}

func externMatch(str string, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(str, pattern[:len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(str, pattern[1:])
	}
	return str == pattern
}

func externMatches(pattern string, str string) (bool, error) {
	return regexp.MatchString(pattern, str)
}

func externStartsWith(str string, prefix string) bool {
	return strings.HasPrefix(str, prefix)
}

func externEndsWith(str string, suffix string) bool {
	return strings.HasSuffix(str, suffix)
}

func externEmptyStringMap() map[string]string {
	return map[string]string{}
}
