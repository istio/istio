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
	"testing"

	"istio.io/istio/mixer/pkg/status"
)

func TestCheckResult(t *testing.T) {
	cr := CheckResult{}
	s := cr.String()
	if s == "" {
		t.Error("Expected valid string")
	}

	if !IsDefault(&cr) {
		t.Error("Expecting IsDefault to return true")
	}

	cr.Status = status.WithCancelled("FAIL")
	if IsDefault(&cr) {
		t.Error("Expecting IsDefault to return false")
	}

	cr = CheckResult{ValidUseCount: 1}
	if IsDefault(&cr) {
		t.Error("Expecting IsDefault to return false")
	}

	cr = CheckResult{ValidDuration: 10}
	if IsDefault(&cr) {
		t.Error("Expecting IsDefault to return false")
	}
}
