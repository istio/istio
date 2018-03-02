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

package v1

import (
	"encoding/json"
	"io/ioutil"
	"strings"
	"testing"
)

func TestUnmarshalLDS(t *testing.T) {
	type ldsResponse struct {
		Listeners Listeners `json:"listeners"`
	}

	files, err := ioutil.ReadDir("testdata")
	if err != nil {
		t.Fatalf(err.Error())
	}
	for _, f := range files {
		n := f.Name()
		if strings.HasPrefix(n, "lds") && strings.HasSuffix(n, ".json.golden") {
			t.Run(n, func(t *testing.T) {
				var lds ldsResponse
				c, err := ioutil.ReadFile("testdata/" + n)
				if err != nil {
					t.Fatalf(err.Error())
				}
				err = json.Unmarshal(c, &lds)
				if err != nil {
					t.Errorf("failed to unmarshal %s: %v", n, err)
				}
			})
		}
	}
}
