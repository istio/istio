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

package util

import (
	"errors"
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"

	"istio.io/istio/operator/pkg/apis/istio/v1alpha1"
)

func TestToYAML(t *testing.T) {
	tests := []struct {
		desc        string
		inVals      interface{}
		expectedOut string
	}{
		{
			desc: "valid-yaml",
			inVals: map[string]interface{}{
				"foo": "bar",
				"yo": map[string]interface{}{
					"istio": "bar",
				},
			},
			expectedOut: `foo: bar
yo:
  istio: bar
`,
		},
		{
			desc: "alphabetical",
			inVals: map[string]interface{}{
				"foo": "yaml",
				"abc": "f",
			},
			expectedOut: `abc: f
foo: yaml
`,
		},
		{
			desc:        "expected-err-nil",
			inVals:      nil,
			expectedOut: "null\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := ToYAML(tt.inVals); got != tt.expectedOut {
				t.Errorf("%s: expected out %v got %s", tt.desc, tt.expectedOut, got)
			}
		})
	}
}

func TestToYAMLWithJSONPB(t *testing.T) {
	tests := []struct {
		desc        string
		in          proto.Message
		expectedOut string
	}{
		{
			desc: "valid-istio-op-with-missing-fields",
			in: &v1alpha1.IstioOperator{
				ApiVersion: "v1",
				Kind:       "operator",
			},
			expectedOut: `apiVersion: v1
kind: operator
metadata:
  creationTimestamp: null
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := ToYAMLWithJSONPB(tt.in); got != tt.expectedOut {
				t.Errorf("%s: expected %v got %v", tt.desc, tt.expectedOut, got)
			}
		})
	}
}

func TestOverlayTrees(t *testing.T) {
	tests := []struct {
		desc            string
		inBase          map[string]interface{}
		inOverlays      map[string]interface{}
		expectedOverlay map[string]interface{}
		expectedErr     error
	}{
		{
			desc: "overlay-valid",
			inBase: map[string]interface{}{
				"foo": "bar",
				"baz": "naz",
			},
			inOverlays: map[string]interface{}{
				"foo": "laz",
			},
			expectedOverlay: map[string]interface{}{
				"baz": "naz",
				"foo": "laz",
			},
			expectedErr: nil,
		},
		{
			desc: "overlay-key-does-not-exist",
			inBase: map[string]interface{}{
				"foo": "bar",
				"baz": "naz",
			},
			inOverlays: map[string]interface{}{
				"i-dont-exist": "i-really-dont-exist",
			},
			expectedOverlay: map[string]interface{}{
				"baz":          "naz",
				"foo":          "bar",
				"i-dont-exist": "i-really-dont-exist",
			},
			expectedErr: nil,
		},
		{
			desc: "remove-key-val",
			inBase: map[string]interface{}{
				"foo": "bar",
			},
			inOverlays: map[string]interface{}{
				"foo": nil,
			},
			expectedOverlay: map[string]interface{}{},
			expectedErr:     nil,
		},
		{
			desc: "expected-err",
			inBase: map[string]interface{}{
				"foo": nil,
			},
			inOverlays:      nil,
			expectedOverlay: nil,
			expectedErr:     errors.New("json merge error (Invalid JSON Patch) for base object"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if gotOverlays, err := OverlayTrees(tt.inBase, tt.inOverlays); !reflect.DeepEqual(gotOverlays, tt.expectedOverlay) ||
				((err != nil && tt.expectedErr == nil) || (err == nil && tt.expectedErr != nil)) {
				t.Errorf("%s: expected overlay & err %v %v got %v %v", tt.desc, tt.expectedOverlay, tt.expectedErr,
					gotOverlays, err)
			}
		})
	}
}

func TestOverlayYAML(t *testing.T) {
	tests := []struct {
		desc    string
		base    string
		overlay string
		expect  string
		err     error
	}{
		{
			desc: "overlay-yaml",
			base: `foo: bar
yo: lo
`,
			overlay: `yo: go`,
			expect: `foo: bar
yo: go
`,
			err: nil,
		},
		{
			desc:    "combine-yaml",
			base:    `foo: bar`,
			overlay: `baz: razmatazz`,
			expect: `baz: razmatazz
foo: bar
`,
			err: nil,
		},
		{
			desc:    "blank",
			base:    `R#)*J#FN`,
			overlay: `FM#)M#F(*#M`,
			expect:  "",
			err:     errors.New("invalid json"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got, err := OverlayYAML(tt.base, tt.overlay); got != tt.expect || ((tt.err != nil && err == nil) || (tt.err == nil && err != nil)) {
				t.Errorf("%s: expected overlay&err %v %v got %v %v", tt.desc, tt.expect, tt.err, got, err)
			}
		})
	}
}

func TestYAMLDiff(t *testing.T) {
	tests := []struct {
		desc   string
		diff1  string
		diff2  string
		expect string
	}{
		{
			desc: "1-line-diff",
			diff1: `hola: yo
foo: bar
goo: tar
`,
			diff2: `hola: yo
foo: bar
notgoo: nottar
`,
			expect: ` foo: bar
-goo: tar
 hola: yo
+notgoo: nottar
 `,
		},
		{
			desc:   "no-diff",
			diff1:  `foo: bar`,
			diff2:  `foo: bar`,
			expect: ``,
		},
		{
			desc:   "invalid-yaml",
			diff1:  `Ij#**#f#`,
			diff2:  `fm*##)n`,
			expect: "error unmarshaling JSON: while decoding JSON: json: cannot unmarshal string into Go value of type map[string]interface {}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := YAMLDiff(tt.diff1, tt.diff2); got != tt.expect {
				t.Errorf("%s: expect %v got %v", tt.desc, tt.expect, got)
			}
		})
	}
}

func TestIsYAMLEqual(t *testing.T) {
	tests := []struct {
		desc   string
		in1    string
		in2    string
		expect bool
	}{
		{
			desc:   "yaml-equal",
			in1:    `foo: bar`,
			in2:    `foo: bar`,
			expect: true,
		},
		{
			desc:   "bad-yaml-1",
			in1:    "O#JF*()#",
			in2:    `foo: bar`,
			expect: false,
		},
		{
			desc:   "bad-yaml-2",
			in1:    `foo: bar`,
			in2:    "#OHJ*#()F",
			expect: false,
		},
		{
			desc: "yaml-not-equal",
			in1: `zinc: iron
stoichiometry: avagadro
`,
			in2: `i-swear: i-am
definitely-not: in1
`,
			expect: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := IsYAMLEqual(tt.in1, tt.in2); got != tt.expect {
				t.Errorf("%v: got %v want %v", tt.desc, got, tt.expect)
			}
		})
	}
}

func TestIsYAMLEmpty(t *testing.T) {
	tests := []struct {
		desc   string
		in     string
		expect bool
	}{
		{
			desc:   "completely-empty",
			in:     "",
			expect: true,
		},
		{
			desc: "comment-logically-empty",
			in: `# this is a comment
# this is another comment that serves no purpose
# (like all comments usually do)
`,
			expect: true,
		},
		{
			desc:   "start-yaml",
			in:     `--- I dont mean anything`,
			expect: true,
		},
		{
			desc: "combine-comments-and-yaml",
			in: `#this is another comment
foo: bar
# ^ that serves purpose
`,
			expect: false,
		},
		{
			desc:   "yaml-not-empty",
			in:     `foo: bar`,
			expect: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := IsYAMLEmpty(tt.in); got != tt.expect {
				t.Errorf("%v: expect %v got %v", tt.desc, tt.expect, got)
			}
		})
	}
}

func TestYAMLReducedDiff(t *testing.T) {
	tests := []struct {
		desc   string
		diff1  string
		diff2  string
		expect string
	}{
		{
			desc: "1-line-diff",
			diff1: `---
hola: yo
foo: bar
goo: tar`,
			diff2: `---
hola: yo
foo: bar
notgoo: nottar`,
			// Cannot use backtick because the expected results contain
			// color control characters which must be escaped. Noticed
			// that the \033[31m are the characters for color red,
			// the \033[32m are the characters for color green.
			expect: " ---\n" +
				" hola: yo\n" +
				" foo: bar\n" +
				"\033[31m-goo: tar\033[0m\n" +
				"\033[32m+notgoo: nottar\033[0m",
		},
		{
			desc:  "empty-to-non-empty-diff",
			diff1: "",
			diff2: `hola: yo
foo: bar
notgoo: nottar`,
			// Cannot use backtick because the expected results contain
			// color control characters which must be escaped. Noticed
			// that the \033[31m are the characters for color red,
			// the \033[32m are the characters for color green.
			expect: "\033[32m+hola: yo\033[0m\n" +
				"\033[32m+foo: bar\033[0m\n" +
				"\033[32m+notgoo: nottar\033[0m",
		},
		{
			desc: "non-empty-to-empty-diff",
			diff1: `hola: yo
foo: bar
notgoo: nottar`,
			diff2: ``,
			// Cannot use backtick because the expected results contain
			// color control characters which must be escaped. Noticed
			// that the \033[31m are the characters for color red,
			// the \033[32m are the characters for color green.
			expect: "\033[31m-hola: yo\033[0m\n" +
				"\033[31m-foo: bar\033[0m\n" +
				"\033[31m-notgoo: nottar\033[0m",
		},
		{
			desc: "multiple-line-same-1-line-diff",
			diff1: `hola: yo
foo: bar
goo: tar
goo1: a
goo2: b
goo3: c
goo4: d
goo5: e`,
			diff2: `hola: yo
foo: bar
notgoo: nottar
goo1: a
goo2: b
goo3: c
goo4: d
goo5: e`,
			// Cannot use backtick because the expected results contain
			// color control characters which must be escaped.
			expect: " hola: yo\n" +
				" foo: bar\n" +
				"\033[31m-goo: tar\033[0m\n" +
				"\033[32m+notgoo: nottar\033[0m\n" +
				` goo1: a
 goo2: b
 goo3: c`,
		},
		{
			desc: "multiple-sections-diff",
			diff1: `---
hola: yo
foo: bar
goo: tar
diff1: firstSection
goo1: a
goo2: b
goo3: c
goo4: d
goo5: e
goo6: a
goo7: b
goo8: c
diff22: secondSection
goo5: e`,
			diff2: `---
hola: yo
foo: bar
goo: tar
diff21: aaa
goo1: a
goo2: b
goo3: c
goo4: d
goo5: e
goo6: a
goo7: b
goo8: c
diff23: d
goo5: e`,
			// Cannot use backtick because the expected results contain
			// color control characters which must be escaped.
			expect: " hola: yo\n" +
				" foo: bar\n" +
				" goo: tar\n" +
				"\033[31m-diff1: firstSection\033[0m\n" +
				"\033[32m+diff21: aaa\033[0m\n" +
				` goo1: a
 goo2: b
 goo3: c
 ...
 goo6: a
 goo7: b
 goo8: c` +
				"\n\033[31m-diff22: secondSection\033[0m\n" +
				"\033[32m+diff23: d\033[0m\n" +
				" goo5: e",
		},
		{
			desc:   "no-diff",
			diff1:  `foo: bar`,
			diff2:  `foo: bar`,
			expect: ``,
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			if got := YAMLReducedDiff(tt.diff1, tt.diff2, 3); got != tt.expect {
				t.Errorf("%s: expect:\n%s \n got:\n%s", tt.desc, tt.expect, got)
			}
		})
	}
}
