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

package store

import (
	"time"

	"istio.io/istio/pkg/config"
)

func IsControlledBy(wle *config.Config, instanceID string) bool {
	return wle.Annotations[WorkloadControllerAnnotation] == instanceID
}

func IsExpired(wle *config.Config, maxConnectionAge, cleanupGracePeriod time.Duration) bool {
	// If there is ConnectedAtAnnotation set, don't cleanup this workload entry.
	// This may happen when the workload fast reconnects to the same istiod.
	// 1. disconnect: the workload entry has been updated
	// 2. connect: but the patch is based on the old workloadentry because of the propagation latency.
	// So in this case the `DisconnectedAtAnnotation` is still there and the cleanup procedure will go on.
	connTime := wle.Annotations[ConnectedAtAnnotation]
	if connTime != "" {
		// handle workload leak when both workload/pilot down at the same time before pilot has a chance to set disconnTime
		connAt, err := time.Parse(timeFormat, connTime)
		if err == nil && uint64(time.Since(connAt)) > uint64(maxConnectionAge) {
			return true
		}
		return false
	}

	disconnTime := wle.Annotations[DisconnectedAtAnnotation]
	if disconnTime == "" {
		return false
	}

	disconnAt, err := time.Parse(timeFormat, disconnTime)
	// if we haven't passed the grace period, don't cleanup
	if err == nil && time.Since(disconnAt) < cleanupGracePeriod {
		return false
	}

	return true
}
