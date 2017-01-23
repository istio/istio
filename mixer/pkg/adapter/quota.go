// Copyright 2017 Google Inc.
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

import "time"

type (
	// QuotaAspect handles quotas and rate limits within the mixer.
	QuotaAspect interface {
		Aspect

		// Alloc allocates the specified amount or fails when not available.
		Alloc(QuotaArgs) (int64, error)

		// AllocBestEffort allocates from 0 to the specified amount, based on availability.
		AllocBestEffort(QuotaArgs) (int64, error)

		// ReleaseBestEffort releases from 0 to the specified amount, based on current usage.
		ReleaseBestEffort(QuotaArgs) (int64, error)
	}

	// QuotaBuilder builds new instances of the Quota aspect.
	QuotaBuilder interface {
		Builder

		// NewQuota returns a new instance of the Quota aspect.
		NewQuota(env Env, c AspectConfig, d map[string]*QuotaDefinition) (QuotaAspect, error)
	}

	// QuotaKind determines the usage semantics
	QuotaKind int8

	// QuotaDefinition is used to describe an individual quota the aspect will encounter at runtime
	QuotaDefinition struct {
		// MaxAmount specifies the upper limit for the quota
		MaxAmount int64

		// Size of rolling window. A value of 0 means no rolling window, allocated quota remains
		// allocated until explicitly released.
		Window time.Duration
	}

	// QuotaArgs supplies the arguments for quota operations.
	QuotaArgs struct {
		// The name of the associated quota definition.
		Name string

		// DeduplicationId is used for deduplicating quota allocation/free calls in the case of
		// failed RPCs and retries. This should be a UUID per call, where the same
		// UUID is used for retries of the same quota allocation or release call.
		DeduplicationID string

		// The amount of quota being allocated or released.
		QuotaAmount int64

		// Labels determine the identity of the quota cell.
		Labels map[string]interface{}
	}
)
