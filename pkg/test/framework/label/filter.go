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

package label

import (
	"fmt"
	"regexp"
	"strings"
)

// Selector is a Set of label filter expressions that get applied together to decide whether tests should be selected
// for execution or not.
type Selector struct {
	// The constraints are and'ed together.
	present Set
	absent  Set
}

var _ fmt.Stringer = Selector{}

func NewSelector(present []Instance, absent []Instance) Selector {
	p := make([]Instance, len(present))
	copy(p, present)

	a := make([]Instance, len(absent))
	copy(a, absent)

	return Selector{
		present: p,
		absent:  a,
	}
}

var userLabelRegex = regexp.MustCompile("^[a-zA-Z]+(\\.[a-zA-Z0-9]+)*$")

func ParseSelector(s string) (Selector, error) {
	var present, absent Set

	parts := strings.Split(s, ",")
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}

		var negative bool
		switch p[0] {
		case '-':
			negative = true
			p = p[1:]
		case '+':
			p = p[1:]
		}

		if !userLabelRegex.Match([]byte(p)) {
			return Selector{}, fmt.Errorf("invalid label name: %q", p)
		}

		l := Instance(p)
		if !all.contains(l) {
			return Selector{}, fmt.Errorf("unknown label name: %q", p)
		}

		if negative {
			if present.contains(l) {
				return Selector{}, fmt.Errorf("duplicate, conflicting label specification: %q", l)
			}
			absent = append(absent, l)
		} else {
			if absent.contains(l) {
				return Selector{}, fmt.Errorf("duplicate, conflicting label specification: %q", l)
			}
			present = append(present, l)
		}
	}

	return NewSelector(present, absent), nil
}

// Selects returns true, if the given label set satisfies the Selector.
func (f *Selector) Selects(inputs Set) bool {
	if inputs.containsAny(f.absent) {
		return false
	}

	if !inputs.containsAll(f.present) {
		return false
	}

	return true
}

// Excludes returns false, if the given set of labels, even combined with new ones, could end up satisfying the Selector.
// It returns false, if Matches would never return true, even if new labels are added to the input set.
func (f *Selector) Excludes(inputs Set) bool {
	if inputs.containsAny(f.absent) {
		return true
	}

	return false
}

func (f Selector) String() string {
	var result string

	for _, p := range f.present {
		if result != "" {
			result += ","
		}
		result += "+" + string(p)
	}

	for _, p := range f.absent {
		if result != "" {
			result += ","
		}
		result += "-" + string(p)
	}

	return result
}
