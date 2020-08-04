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

// NewSelector returns a new selector based on the given presence/absence predicates.
func NewSelector(present []Instance, absent []Instance) Selector {
	return Selector{
		present: NewSet(present...),
		absent:  NewSet(absent...),
	}
}

var userLabelRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z0-9_]+)*$`)

// ParseSelector parses and returns a new instance of Selector.
func ParseSelector(s string) (Selector, error) {
	var present, absent []Instance

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
			absent = append(absent, l)
		} else {
			present = append(present, l)
		}
	}

	pSet := NewSet(present...)
	aSet := NewSet(absent...)

	if pSet.containsAny(aSet) || aSet.containsAny(pSet) {
		return Selector{}, fmt.Errorf("conflicting selector specification: %q", s)
	}

	return NewSelector(present, absent), nil
}

// Selects returns true, if the given label set satisfies the Selector.
func (f *Selector) Selects(inputs Set) bool {
	return !inputs.containsAny(f.absent) && inputs.containsAll(f.present)
}

// Excludes returns false, if the given set of labels, even combined with new ones, could end up satisfying the Selector.
// It returns false, if Matches would never return true, even if new labels are added to the input set.
func (f *Selector) Excludes(inputs Set) bool {
	return inputs.containsAny(f.absent)
}

func (f Selector) String() string {
	var result string

	for _, p := range f.present.All() {
		if result != "" {
			result += ","
		}
		result += "+" + string(p)
	}

	for _, p := range f.absent.All() {
		if result != "" {
			result += ","
		}
		result += "-" + string(p)
	}

	return result
}
