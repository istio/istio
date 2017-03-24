// Copyright 2017 The Istio Authors.
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

package adapterManager

import (
	"testing"
)

func TestMethodNames(t *testing.T) {
	cases := []struct {
		method apiMethod
		name   string
	}{
		{checkMethod, "Check"},
		{reportMethod, "Report"},
		{quotaMethod, "Quota"},
	}

	for _, c := range cases {
		if c.method.String() != c.name {
			t.Errorf("Got %s, expecting %s", c.method.String(), c.name)
		}
	}
}
