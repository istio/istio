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

package table

import (
	"bytes"
	"testing"

	"github.com/fatih/color"
)

type testObject struct {
	name      string
	namespace string
	version   string
}

var newTestObject = func(name, namespace, version string) testObject {
	return testObject{
		name:      name,
		namespace: namespace,
		version:   version,
	}
}

func TestWriter(t *testing.T) {
	got := &bytes.Buffer{}
	w := NewStyleWriter(got)
	w.AddHeader("NAME", "NAMESPACE", "VERSION")
	w.SetAddRowFunc(func(obj interface{}) Row {
		o := obj.(testObject)
		return Row{
			Cells: []Cell{
				NewCell(o.name),
				NewCell(o.namespace, color.FgGreen),
				NewCell(o.version),
			},
		}
	})
	w.AddRow(newTestObject("foo", "bar", "1.0"))
	w.AddRow(newTestObject("baz", "qux", "2.0"))
	w.AddRow(newTestObject("qux", "quux", "3"))
	w.Flush()
	expected := "NAME  NAMESPACE      VERSION\n" +
		"foo   \x1b[32mbar\x1b[0m            1.0\n" +
		"baz   \x1b[32mqux\x1b[0m            2.0\n" +
		"qux   \x1b[32mquux\x1b[0m           3\n"
	if got.String() != expected {
		t.Errorf("got %q, want %q", got.String(), expected)
	}
}
