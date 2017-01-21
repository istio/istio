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

type (
	// QuotaAspect is the interface for adapters that will handle quota management
	// within the mixer.
	QuotaAspect interface {
		Aspect

		// Alloc allocates the specified amount or fails when not available.
		Alloc(QuotaArgs) (int64, error)

		// AllocBestEffort allocates from 0 to the specified amount, based on availability.
		AllocBestEffort(QuotaArgs) (int64, error)

		// ReleaseBestEffort releases from 0 to the specified amount, based on current usage.
		ReleaseBestEffort(QuotaArgs) (int64, error)
	}

	// QuotaAdapter is the interface for building Aspect instances for mixer
	// quota backends.
	QuotaAdapter interface {
		Adapter

		// NewQuota returns a new quota implementation, based on the
		// supplied Aspect configuration for the backend.
		NewQuota(env Env, c AspectConfig) (QuotaAspect, error)
	}

	// QuotaArgs supplies the arguments for quota operations.
	QuotaArgs struct {
		// DeduplicationId is used for deduplicating quota allocation/free calls in the case of
		// failed RPCs and retries. This should be a UUID per call, where the same
		// UUID is used for retries of the same quota allocation or release call.
		DeduplicationID string

		// The amount of quota being allocated or released.
		QuotaAmount int64

		// Attributes determine the identity of the quota cell.
		Attributes map[string]interface{}
	}
)
