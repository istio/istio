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

package svcctrl

var supportedMetrics = []metricDef{
	// Producer destination metrics
	{
		name:           "serviceruntime.googleapis.com/api/producer/request_count",
		valueGenerator: generateRequestCount,
		labels: []string{
			"/protocol",
			"/response_code",
			"/response_code_class",
			"/status_code"},
	},
	{
		name:           "serviceruntime.googleapis.com/api/producer/backend_latencies",
		valueGenerator: generateBackendLatencies,
		labels:         []string{},
	},
	// by consumer metrics
	{
		name:           "serviceruntime.googleapis.com/api/producer/by_consumer/request_count",
		valueGenerator: generateRequestCount,
		labels: []string{
			"/credential_id",
			"/protocol",
			"/response_code",
			"/response_code_class",
			"/status_code",
		},
	},
	// Consumer destination metrics
	{
		name:           "serviceruntime.googleapis.com/api/consumer/request_count",
		valueGenerator: generateRequestCount,
		labels: []string{
			"/credential_id",
			"/protocol",
			"/response_code",
			"/response_code_class",
			"/status_code",
		},
	},
	{
		name:           "serviceruntime.googleapis.com/api/consumer/backend_latencies",
		valueGenerator: generateBackendLatencies,
		labels:         []string{"/credential_id"},
	},
}
