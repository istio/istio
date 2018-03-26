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

package dispatcher

import (
	"strings"
	"testing"

	"istio.io/istio/mixer/pkg/attribute"
)

func TestGetIdentityAttributeValue(t *testing.T) {
	bag := attribute.GetFakeMutableBagForTesting(map[string]interface{}{
		"ident":     "value",
		"nonstring": 23,
	})

	result, err := getIdentityAttributeValue(bag, "ident")
	if err != nil {
		t.Fail()
	}
	if result != "value" {
		t.Fail()
	}

	_, err = getIdentityAttributeValue(bag, "nonstring")
	if err == nil {
		t.Fail()
	}

	_, err = getIdentityAttributeValue(bag, "nonexistent")
	if err == nil {
		t.Fail()
	}
}

func TestGetNamespace(t *testing.T) {
	tests := []struct {
		dest string
		ns   string
	}{
		{dest: "", ns: ""},
		{dest: "foo", ns: ""},
		{dest: "foo.bar", ns: "bar"},
		{dest: "foo.bar.baz", ns: "bar"},
	}

	for _, tst := range tests {
		t.Run(tst.dest, func(tt *testing.T) {
			// Compare it to the original algorithm
			actual := ""
			splits := strings.SplitN(tst.dest, ".", 3) // we only care about service and namespace.
			if len(splits) > 1 {
				actual = splits[1]
			}
			if actual != tst.ns {
				tt.Fatalf("'%s' != '%s' (Original)", actual, tst.ns)
			}

			actual = getNamespace(tst.dest)
			if actual != tst.ns {
				tt.Fatalf("'%s' != '%s'", actual, tst.ns)
			}
		})
	}
}
