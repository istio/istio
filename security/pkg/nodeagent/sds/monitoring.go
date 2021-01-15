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

package sds

import (
	"istio.io/pkg/monitoring"
)

// TODO(JimmyCYJ): Create an endpoint in Citadel agent to expose metrics.
// Previously exposed metrics: pending_push_per_connection, stale_conn_count_per_connection,
// pushes_per_connection, push_errors_per_connection, pushed_root_cert_expiry_timestamp, pushed_server_cert_expiry_timestamp
var (
	// totalPushCounts records total number of SDS pushes since server starts serving.
	totalPushCounts = monitoring.NewSum(
		"total_pushes",
		"The total number of SDS pushes.",
	)

	// totalPushErrorCounts records total number of failed SDS pushes since server starts serving.
	totalPushErrorCounts = monitoring.NewSum(
		"total_push_errors",
		"The total number of failed SDS pushes.",
	)

	// totalActiveConnCounts records total number of active SDS connections.
	totalActiveConnCounts = monitoring.NewSum(
		"total_active_connections",
		"The total number of active SDS connections.",
	)

	// totalStaleConnCounts records total number of stale SDS connections.
	totalStaleConnCounts = monitoring.NewSum(
		"total_stale_connections",
		"The total number of stale SDS connections.",
	)

	// totalSecretUpdateFailureCounts records total number of secret update failures reported by
	// proxy in SDS request <error_detail> field.
	totalSecretUpdateFailureCounts = monitoring.NewSum(
		"total_secret_update_failures",
		"The total number of dynamic secret update failures reported by proxy.",
	)
)

func init() {
	monitoring.MustRegister(
		totalPushCounts,
		totalPushErrorCounts,
		totalActiveConnCounts,
		totalStaleConnCounts,
		totalSecretUpdateFailureCounts,
	)
}
