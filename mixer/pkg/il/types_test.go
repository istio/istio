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

package il

import (
	"testing"
)

func TestTypeString(t *testing.T) {
	for k, v := range typeNames {
		if k.String() != v {
			t.Fatal("Should have returned type name")
		}
	}
}

func TestGetType(t *testing.T) {
	for k, v := range typesByName {
		if tt, e := GetType(k); !e || tt != v {
			t.Fatal("Should have returned the type")
		}
	}
}
