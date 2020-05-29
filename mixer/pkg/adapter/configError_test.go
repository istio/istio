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

package adapter

import (
	"fmt"
	"testing"
)

func TestConfigErrors(t *testing.T) {
	cases := []struct {
		field      string
		underlying string
		error      string
	}{
		{"F0", "format 0", "F0: format 0"},
		{"F1", "format 1", "F1: format 1"},
	}

	var ce *ConfigErrors
	ce = ce.Appendf(cases[0].field, "format %d", 0)
	ce = ce.Append(cases[1].field, fmt.Errorf("format %d", 1))

	if ce.Error() != ce.String() {
		t.Errorf("ce.String() = '%s', expected '%s'", ce.String(), ce.Error())
	}

	for i, c := range cases {
		err := ce.Multi.Errors[i].(ConfigError)
		if err.Field != c.field {
			t.Errorf("Case %d field is '%s', expected '%s'", i, err.Field, c.field)
		}
		if err.Underlying.Error() != c.underlying {
			t.Errorf("Case %d underlying is '%s', expected '%s'", i, err.Underlying.Error(), c.underlying)
		}
		if err.Error() != c.error {
			t.Errorf("Case %d Error() returns '%s', expected '%s'", i, err.Error(), c.error)
		}
		if err.String() != err.Error() {
			t.Errorf("err.String() = '%s', expected '%s'", err.String(), err.Error())
		}
	}

	t.Log(ce)
}

func TestNil(t *testing.T) {
	var ce *ConfigErrors
	ce = ce.Appendf("Foo", "format %d", 0)
	if ce == nil {
		t.Error("Expecting object to be allocated on first use")
	}

	ce = nil
	ce = ce.Append("Foo", fmt.Errorf("format %d", 0))
	if ce == nil {
		t.Error("Expecting object to be allocated on first use")
	}

	ce = nil
	if ce.Error() != "" {
		t.Error("Expected empty error string")
	}
}

func TestExtend(t *testing.T) {
	var c1 *ConfigErrors
	c1 = c1.Appendf("Foo1", "format %d", 0)
	c1 = c1.Appendf("Foo2", "format %d", 1)
	lenc1 := len(c1.Multi.Errors)

	var c2 *ConfigErrors
	c2 = c2.Appendf("Foo1", "format %d", 0)
	c2 = c2.Appendf("Foo2", "format %d", 1)
	lenc2 := len(c2.Multi.Errors)

	c2 = c2.Extend(c1)

	if len(c2.Multi.Errors) != lenc1+lenc2 {
		t.Errorf("Expected length: %d actual %d", lenc1+lenc2, len(c2.Multi.Errors))
	}

	c1 = nil
	c1 = c1.Extend(c2)
	if len(c1.Multi.Errors) != len(c2.Multi.Errors) {
		t.Error("Expected lengths to match")
	}

	var ce *ConfigErrors
	if ce.Extend(nil) != nil {
		t.Error("nil.Extend(nil) != nil")
	}

	ce = ce.Append("Foo", fmt.Errorf("format %d", 0))
	if ce.Extend(nil) != ce {
		t.Error("ce.Extend(nil) != ce")
	}
}
