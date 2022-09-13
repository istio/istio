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

package fixtures

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// ExpectEqual calls CheckEqual and fails the test if it returns an error.
func ExpectEqual(t *testing.T, o1 any, o2 any) {
	t.Helper()
	if err := CheckEqual(o1, o2); err != nil {
		t.Fatal(err)
	}
}

// CheckEqual checks that o1 and o2 are equal. If not, returns an error with the diff.
func CheckEqual(o1 any, o2 any) error {
	if diff := cmp.Diff(o1, o2); diff != "" {
		return fmt.Errorf(diff)
	}
	return nil
}
