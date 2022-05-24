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
	"strconv"
	"testing"
)

func TestLabels(t *testing.T) {
	tests := []struct {
		filter   string
		labels   Set
		expected bool
		err      bool
	}{
		{filter: "", labels: nil, expected: true},
		{filter: "", labels: NewSet(Postsubmit), expected: true},
		{filter: "", labels: NewSet(Postsubmit, CustomSetup), expected: true},
		{filter: "$requires.kube", labels: NewSet(Postsubmit, CustomSetup), err: true},
		{filter: "zoo", labels: NewSet(Postsubmit, CustomSetup), expected: true},
		{filter: "postsubmit", labels: NewSet(Postsubmit), expected: true},
		{filter: "postsubmit", labels: NewSet(CustomSetup), expected: false},
		{filter: "postsubmit", labels: NewSet(CustomSetup, Postsubmit), expected: true},
		{filter: "postsubmit,customsetup", labels: NewSet(Postsubmit, CustomSetup), expected: true},
		{filter: "postsubmit,customsetup", labels: NewSet(Postsubmit), expected: false},
		{filter: "+postsubmit,+customsetup", labels: NewSet(Postsubmit), expected: false},
		{filter: "postsubmit,+customsetup", labels: NewSet(Postsubmit, CustomSetup), expected: true},
		{filter: "-postsubmit", labels: NewSet(), expected: true},
		{filter: "-postsubmit", labels: NewSet(Postsubmit), expected: false},
		{filter: "-postsubmit,-customsetup", labels: NewSet(), expected: true},
		{filter: "-postsubmit,customsetup", labels: NewSet(Postsubmit, CustomSetup), expected: false},
		{filter: "-postsubmit,customsetup", labels: NewSet(CustomSetup), expected: true},
		{filter: "-postsubmit,customsetup", labels: NewSet(Postsubmit), expected: false},
		{filter: "-postsubmit,customsetup", labels: NewSet(), expected: false},
		{filter: "-postsubmit,-postsubmit", labels: NewSet(), expected: true},
		{filter: "-postsubmit,-postsubmit", labels: NewSet(Postsubmit), expected: false},
		{filter: "postsubmit,postsubmit", labels: NewSet(), expected: false},
		{filter: "postsubmit,postsubmit", labels: NewSet(Postsubmit), expected: true},
		{filter: "postsubmit,postsubmit", labels: NewSet(), expected: false},
		{filter: "postsubmit,-postsubmit", labels: NewSet(), err: true},
	}

	for i, te := range tests {
		t.Run(strconv.FormatInt(int64(i), 10), func(t *testing.T) {
			f, err := ParseSelector(te.filter)
			if err != nil {
				if te.err {
					return
				}
				t.Fatalf("Unexpected error: %v, filter:%q, labels:%v", err, te.filter, te.labels)
			} else if te.err {
				t.Fatalf("Expected error not found: filter:%q, labels:%v", te.filter, te.labels)
			}

			actual := f.Selects(te.labels)
			if actual != te.expected {
				t.Fatalf("Mismatch: got:%v, wanted: %v, filter:%q, labels:%v", actual, te.expected, te.filter, te.labels)
			}
		})
	}
}
