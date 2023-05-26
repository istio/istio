// Copyright Istio Authors
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

package labels

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
)

const (
	DNS1123LabelMaxLength = 63 // Public for testing only.
	dns1123LabelFmt       = "[a-zA-Z0-9](?:[-a-zA-Z0-9]*[a-zA-Z0-9])?"
	// a wild-card prefix is an '*', a normal DNS1123 label with a leading '*' or '*-', or a normal DNS1123 label
	wildcardPrefix = `(\*|(\*|\*-)?` + dns1123LabelFmt + `)`

	// Using kubernetes requirement, a valid key must be a non-empty string consist
	// of alphanumeric characters, '-', '_' or '.', and must start and end with an
	// alphanumeric character (e.g. 'MyValue',  or 'my_value',  or '12345'
	qualifiedNameFmt = "(?:[A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]"

	// In Kubernetes, label names can start with a DNS name followed by a '/':
	dnsNamePrefixFmt       = dns1123LabelFmt + `(?:\.` + dns1123LabelFmt + `)*/`
	dnsNamePrefixMaxLength = 253
)

var (
	tagRegexp            = regexp.MustCompile("^(" + dnsNamePrefixFmt + ")?(" + qualifiedNameFmt + ")$") // label value can be an empty string
	labelValueRegexp     = regexp.MustCompile("^" + "(" + qualifiedNameFmt + ")?" + "$")
	dns1123LabelRegexp   = regexp.MustCompile("^" + dns1123LabelFmt + "$")
	wildcardPrefixRegexp = regexp.MustCompile("^" + wildcardPrefix + "$")
)

// Instance is a non empty map of arbitrary strings. Each version of a service can
// be differentiated by a unique set of labels associated with the version. These
// labels are assigned to all instances of a particular service version. For
// example, lets say catalog.mystore.com has 2 versions v1 and v2. v1 instances
// could have labels gitCommit=aeiou234, region=us-east, while v2 instances could
// have labels name=kittyCat,region=us-east.
type Instance map[string]string

// SubsetOf is true if the label has same values for the keys
func (i Instance) SubsetOf(that Instance) bool {
	if len(i) == 0 {
		return true
	}

	if len(that) == 0 || len(that) < len(i) {
		return false
	}

	for k, v1 := range i {
		if v2, ok := that[k]; !ok || v1 != v2 {
			return false
		}
	}
	return true
}

// Match is true if the label has same values for the keys.
// if len(i) == 0, will return false. It is mainly used for service -> workload
func (i Instance) Match(that Instance) bool {
	if len(i) == 0 {
		return false
	}

	return i.SubsetOf(that)
}

// Equals returns true if the labels are equal.
func (i Instance) Equals(that Instance) bool {
	return maps.Equal(i, that)
}

// Validate ensures tag is well-formed
func (i Instance) Validate() error {
	if i == nil {
		return nil
	}
	var errs error
	for k, v := range i {
		if err := validateTagKey(k); err != nil {
			errs = multierror.Append(errs, err)
		}
		if !labelValueRegexp.MatchString(v) {
			errs = multierror.Append(errs, fmt.Errorf("invalid tag value: %q", v))
		}
	}
	return errs
}

// IsDNS1123Label tests for a string that conforms to the definition of a label in
// DNS (RFC 1123).
func IsDNS1123Label(value string) bool {
	return len(value) <= DNS1123LabelMaxLength && dns1123LabelRegexp.MatchString(value)
}

// IsWildcardDNS1123Label tests for a string that conforms to the definition of a label in DNS (RFC 1123), but allows
// the wildcard label (`*`), and typical labels with a leading astrisk instead of alphabetic character (e.g. "*-foo")
func IsWildcardDNS1123Label(value string) bool {
	return len(value) <= DNS1123LabelMaxLength && wildcardPrefixRegexp.MatchString(value)
}

// validateTagKey checks that a string is valid as a Kubernetes label name.
func validateTagKey(k string) error {
	match := tagRegexp.FindStringSubmatch(k)
	if match == nil {
		return fmt.Errorf("invalid tag key: %q", k)
	}

	if len(match[1]) > 0 {
		dnsPrefixLength := len(match[1]) - 1 // exclude the trailing / from the length
		if dnsPrefixLength > dnsNamePrefixMaxLength {
			return fmt.Errorf("invalid tag key: %q (DNS prefix is too long)", k)
		}
	}

	if len(match[2]) > DNS1123LabelMaxLength {
		return fmt.Errorf("invalid tag key: %q (name is too long)", k)
	}

	return nil
}

func (i Instance) String() string {
	// Ensure stable ordering
	keys := slices.Sort(maps.Keys(i))

	var buffer strings.Builder
	// Assume each kv pair is roughly 25 characters. We could be under or over, this is just a guess to optimize
	buffer.Grow(len(keys) * 25)
	first := true
	for _, k := range keys {
		v := i[k]
		if !first {
			buffer.WriteString(",")
		} else {
			first = false
		}
		if len(v) > 0 {
			buffer.WriteString(k + "=" + v)
		} else {
			buffer.WriteString(k)
		}
	}
	return buffer.String()
}
