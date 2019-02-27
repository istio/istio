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

package model

import (
	"time"
)

// WorkloadCertArgs is configuration for the workload.
type WorkloadCertArgs struct {
	WorkloadCertTTL time.Duration

	// The length of certificate rotation grace period, configured as the ratio of the certificate TTL.
	WorkloadCertGracePeriodRatio float32

	// The minimum grace period for workload cert rotation.
	WorkloadCertMinGracePeriod time.Duration
}

// DefaultWorkloadCertArgs allocates a Config struct initialized.
func DefaultWorkloadCertArgs() *WorkloadCertArgs {
	return &WorkloadCertArgs{
		WorkloadCertTTL:              90 * 24 * time.Hour,
		WorkloadCertGracePeriodRatio: 0.5,
		WorkloadCertMinGracePeriod:   10 * time.Minute,
	}
}
