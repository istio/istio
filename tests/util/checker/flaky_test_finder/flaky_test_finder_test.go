// Copyright 2018 Istio Authors. All Rights Reserved.
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

package flakytestfinder

import (
	"reflect"
	"testing"
)

func TestIntegTestSkipByIssueRule(t *testing.T) {
	rpts, _ := ReportFlakyTests([]string{"testdata/"})
	expectedRpts := []string{"TestIsMarkedFlaky1", "TestIsMarkedFlaky2"}

	if !reflect.DeepEqual(rpts, expectedRpts) {
		t.Errorf("reports don't match\nReceived: %v\nExpected: %v", rpts, expectedRpts)
	}
}
