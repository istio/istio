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

// Package metrics defines metrics and monitoring functionality
// used throughout operator.
package metrics

import (
	"istio.io/pkg/monitoring"
)

var (
	// MDPVersionLabel describes version of running binary.
	MDPVersionLabel = monitoring.MustCreateLabel("version")

	// CRFetchErrorReasonLabel describes the reason/HTTP code
	// for failing to fetch CR.
	CRFetchErrorReasonLabel = monitoring.MustCreateLabel("reason")
)

var (
	// Version is the version of the operator binary running currently.
	// This is required for fleet level metrics although it is available from
	// ControlZ (more precisely versionz endpoint).
	Version = monitoring.NewGauge(
		"version",
		"Version of MDP binary",
		monitoring.WithLabels(MDPVersionLabel),
	)

	// GetCRErrorTotal counts the number of times fetching
	// CR fails from API server.
	GetCRErrorTotal = monitoring.NewSum(
		"get_cr_error_total",
		"Number of times fetching CR from apiserver failed",
		monitoring.WithLabels(CRFetchErrorReasonLabel),
	)
)

func init() {
	monitoring.MustRegister(
		Version,

		GetCRErrorTotal,
	)
}
