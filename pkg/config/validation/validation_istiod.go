//go:build !agent

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

package validation

import (
	"fmt"

	"github.com/google/cel-go/cel"

	telemetry "istio.io/api/telemetry/v1alpha1"
)

var celEnv, _ = cel.NewEnv()

func validateTelemetryFilter(filter *telemetry.AccessLogging_Filter) error {
	_, issue := celEnv.Parse(filter.Expression)
	if issue.Err() != nil {
		return fmt.Errorf("must be a valid CEL expression, %w", issue.Err())
	}

	return nil
}
