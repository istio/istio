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
	"fmt"

	"istio.io/api/mixer/adapter/model/v1beta1"
	"istio.io/istio/mixer/pkg/status"
)

// CheckResult provides return value from check request call on the handler.
type CheckResult = v1beta1.CheckResult

// IsDefault returns true if the CheckResult is in its zero state
func IsDefault(r *CheckResult) bool {
	return status.IsOK(r.Status) && r.ValidDuration == 0 && r.ValidUseCount == 0
}

// String returns human readable text for the check result.
func String(r *CheckResult) string {
	return fmt.Sprintf("CheckResult: status:%s, duration:%d, usecount:%d", status.String(r.Status), r.ValidDuration, r.ValidUseCount)

}
