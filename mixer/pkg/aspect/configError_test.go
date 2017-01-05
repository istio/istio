// Copyright 2017 Google Inc.
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

package aspect

import (
	"fmt"
	"testing"
)

func TestConfigErrors(t *testing.T) {
	cases := []struct {
		field string
		underlying string
		error string
	}{
		{"F0", "Format 0", "F0: Format 0"},
		{"F1", "Format 1", "F1: Format 1"},
	}

	var ce *ConfigErrors
	ce = ce.Appendf(cases[0].field, "Format %d", 0)
	ce = ce.Append(cases[1].field, fmt.Errorf("Format %d", 1))

	for i, c := range cases {
		err := ce.Multi.Errors[i].(ConfigError)
		if err.Field != c.field {
			t.Errorf("Error %d field is '%s', expected '%s'", i, err.Field, c.field)
		}
		if err.Underlying.Error() != c.underlying {
			t.Errorf("Error %d underlying is '%s', expected '%s'", i, err.Underlying.Error(), c.underlying)
		}
		if err.Error() != c.error {
			t.Errorf("Error %d Error() returns '%s', expected '%s'", i, err.Error(), c.error)
		}
	}

	t.Log(ce)
}
