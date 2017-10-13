// Copyright 2017 Istio Authors
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

package na

import (
	"testing"
)

func TestNewNodeAgent(t *testing.T) {

	testCases := map[string]struct {
		config      *Config
		expectedErr string
	}{
		"null config test": {
			config:      nil,
			expectedErr: "Nil configuration passed",
		},
		"onprem env test": {
			config: &Config{
				Env: "onprem",
			},
			expectedErr: "",
		},
		"gcp env test": {
			config: &Config{
				Env: "gcp",
			},
			expectedErr: "",
		},
		"Unsupported env test": {
			config: &Config{
				Env: "somethig else",
			},
			expectedErr: "Invalid env somethig else specified",
		},
	}

	for id, c := range testCases {
		_, err := NewNodeAgent(c.config)

		if len(c.expectedErr) > 0 {
			if err == nil {
				t.Errorf("%s: Succeeded. Error expected: %v", id, err)
			} else if err.Error() != c.expectedErr {
				t.Errorf("%s: incorrect error message: %s VS %s",
					id, err.Error(), c.expectedErr)
			}
		} else if err != nil {
			t.Errorf("%s: Unexpected Error: %v", id, err)
		}

	}
}
