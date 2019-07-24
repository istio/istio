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

// This file describes the abstract model of services (and their instances) as
// represented in Istio. This model is independent of the underlying platform
// (Kubernetes, Mesos, etc.). Platform specific adapters found populate the
// model object with various fields, from the metadata found in the platform.
// The platform independent proxy code uses the representation in the model to
// generate the configuration files for the Layer 7 proxy sidecar. The proxy
// code is specific to individual proxy implementations

package config

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/go-multierror"
)

// Labels is a non empty set of arbitrary strings. Each version of a service can
// be differentiated by a unique set of labels associated with the version. These
// labels are assigned to all instances of a particular service version. For
// example, lets say catalog.mystore.com has 2 versions v1 and v2. v1 instances
// could have labels gitCommit=aeiou234, region=us-east, while v2 instances could
// have labels name=kittyCat,region=us-east.
type Labels map[string]string

// SubsetOf is true if the label has identical values for the keys
func (l Labels) SubsetOf(that Labels) bool {
	for k, v := range l {
		if that[k] != v {
			return false
		}
	}
	return true
}

// Equals returns true if the labels are identical
func (l Labels) Equals(that Labels) bool {
	if l == nil {
		return that == nil
	}
	if that == nil {
		return l == nil
	}
	return l.SubsetOf(that) && that.SubsetOf(l)
}

// Validate ensures tag is well-formed
func (l Labels) Validate() error {
	var errs error
	for k, v := range l {
		if !tagRegexp.MatchString(k) {
			errs = multierror.Append(errs, fmt.Errorf("invalid tag key: %q", k))
		}
		if !labelValueRegexp.MatchString(v) {
			errs = multierror.Append(errs, fmt.Errorf("invalid tag value: %q", v))
		}
	}
	return errs
}

func (l Labels) String() string {
	labels := make([]string, 0, len(l))
	for k, v := range l {
		if len(v) > 0 {
			labels = append(labels, fmt.Sprintf("%s=%s", k, v))
		} else {
			labels = append(labels, k)
		}
	}
	sort.Strings(labels)

	var buffer bytes.Buffer
	var first = true
	for _, label := range labels {
		if !first {
			buffer.WriteString(",")
		} else {
			first = false
		}
		buffer.WriteString(label)
	}
	return buffer.String()
}

// ParseLabelsString extracts labels from a string
func ParseLabelsString(s string) Labels {
	pairs := strings.Split(s, ",")
	tag := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		kv := strings.Split(pair, "=")
		if len(kv) > 1 {
			tag[kv[0]] = kv[1]
		} else {
			tag[kv[0]] = ""
		}
	}
	return tag
}
