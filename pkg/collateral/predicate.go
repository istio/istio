// Copyright 2019 Istio Authors
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

package collateral

import (
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/monitoring"
)

// Predicates are a set of predicates to apply when generating collaterals.
type Predicates struct {
	SelectEnv    SelectEnvFn
	SelectMetric SelectMetricFn
}

// SelectEnvFn is a predicate function for selecting environment variables when generating collateral documents.
type SelectEnvFn func(env.Var) bool

// SelectMetricFn is a predicate function for selecting metrics when generating collateral documents.
type SelectMetricFn func(monitoring.MetricDefinition) bool

// DefaultSelectEnvFn is used to select all environment variables.
func DefaultSelectEnvFn(_ env.Var) bool {
	return true
}

// DefaultSelectMetricFn is used to select all metrics.
func DefaultSelectMetricFn(_ monitoring.MetricDefinition) bool {
	return true
}
