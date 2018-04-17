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

package adapter

import (
	"reflect"
	"testing"
	"time"
)

func TestResult_Combine(t *testing.T) {
	// checkresult combine results to use the minimum Validation count
	// and validation duration.
	for _, c := range []struct {
		this  *CheckResult
		other Result
		ans   Result
	}{{&CheckResult{ValidUseCount: 10, ValidDuration: 2 * time.Hour},
		&CheckResult{ValidUseCount: 5, ValidDuration: 20 * time.Hour},
		&CheckResult{ValidUseCount: 5, ValidDuration: 2 * time.Hour}},
		{&CheckResult{ValidUseCount: 2, ValidDuration: 30 * time.Hour},
			&CheckResult{ValidUseCount: 5, ValidDuration: 20 * time.Hour},
			&CheckResult{ValidUseCount: 2, ValidDuration: 20 * time.Hour}},
	} {
		c.this.Combine(c.other)
		if !reflect.DeepEqual(c.this, c.ans) {
			t.Errorf("got %v, want %v", c.this, c.ans)
		}
	}
}
