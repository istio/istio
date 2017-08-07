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

package config

import (
	"fmt"

	"istio.io/mixer/pkg/pool"
)

// Kind of aspect
type Kind uint

// KindSet is a set of aspects by kind.
type KindSet uint

// Supported kinds of aspects
const (
	Unspecified Kind = iota
	AccessLogsKind
	ApplicationLogsKind
	AttributesKind
	DenialsKind
	ListsKind
	MetricsKind
	QuotasKind

	NumKinds
)

// Name of all supported aspect kinds.
const (
	AccessLogsKindName      = "access-logs"
	ApplicationLogsKindName = "application-logs"
	AttributesKindName      = "attributes"
	DenialsKindName         = "denials"
	ListsKindName           = "lists"
	MetricsKindName         = "metrics"
	QuotasKindName          = "quotas"
)

// kindToString maps from kinds to their names.
var kindToString = map[Kind]string{
	AccessLogsKind:      AccessLogsKindName,
	ApplicationLogsKind: ApplicationLogsKindName,
	AttributesKind:      AttributesKindName,
	DenialsKind:         DenialsKindName,
	ListsKind:           ListsKindName,
	MetricsKind:         MetricsKindName,
	QuotasKind:          QuotasKindName,
}

// stringToKinds maps from kind name to kind enum.
var stringToKind = map[string]Kind{}

// String returns the string representation of the kind, or "" if an unknown kind is given.
func (k Kind) String() string {
	return kindToString[k]
}

// ParseKind converts a string into a Kind.
func ParseKind(s string) (Kind, bool) {
	k, found := stringToKind[s]
	return k, found
}

// IsSet tests whether the given kind is enabled in the set.
func (ks KindSet) IsSet(k Kind) bool {
	return ((1 << k) & ks) != 0
}

// Set returns a new KindSet with the given aspect kind enabled.
func (ks KindSet) Set(k Kind) KindSet {
	return ks | (1 << k)
}

// make gas happy
func ignoreErrors(args ...interface{}) {}

func (ks KindSet) String() string {
	buf := pool.GetBuffer()
	defer pool.PutBuffer(buf) // fine to pay the defer overhead; this is out of request path

	ignoreErrors(fmt.Fprint(buf, "["))
	for k, v := range kindToString {
		if ks.IsSet(k) {
			ignoreErrors(fmt.Fprintf(buf, "%s, ", v))
		}
	}

	// Make sure the empty KindSet is printed as "[]" rather than "".
	if buf.Len() <= len("[") {
		return "[]"
	}
	// Otherwise trim off the trailing ", " and close the bracket we opened.
	buf.Truncate(buf.Len() - 2)
	ignoreErrors(fmt.Fprint(buf, "]"))
	return buf.String()
}

func init() {
	for k, v := range kindToString {
		stringToKind[v] = k
	}
}
