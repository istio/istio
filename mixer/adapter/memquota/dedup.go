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

package memquota

import (
	"sync"
	"time"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/template/quota"
)

type (
	// dedupUtil stores common information for in-memory and redis quota adapters
	dedupUtil struct {
		sync.Mutex

		// two ping-ponging maps of active dedup ids
		recentDedup map[string]dedupState
		oldDedup    map[string]dedupState

		// used for reaping dedup ids
		ticker *time.Ticker

		// indirection to support fast deterministic tests
		getTime func() time.Time

		logger adapter.Logger
	}

	// dedupState maintains a dedup state
	dedupState struct {
		amount int64
		exp    time.Time
	}

	quotaFunc func(key string, currentTime time.Time, currentTick int64) (int64, time.Time, time.Duration)
)

const (
	// ticksPerSecond determines the number of quantized time intervals in 1 second.
	ticksPerSecond = 10

	// nanosPerTick is equal to ns/tick
	nanosPerTick = int64(time.Second / ticksPerSecond)
)

// handleDedup is a wrapper function that handles dedupping semantics.
func (du *dedupUtil) handleDedup(instance *quota.Instance, args adapter.QuotaArgs, qf quotaFunc) (int64, time.Duration, string, error) {
	key := makeKey(instance.Name, instance.Dimensions)

	du.Lock()

	currentTime := du.getTime()
	currentTick := currentTime.UnixNano() / nanosPerTick

	var amount int64
	var t time.Time
	var exp time.Duration

	result, dup := du.recentDedup[args.DeduplicationID]
	if !dup {
		result, dup = du.oldDedup[args.DeduplicationID]
	}

	if dup {
		amount = result.amount
		exp = result.exp.Sub(currentTime)
		if exp < 0 {
			exp = 0
		}
	} else {
		amount, t, exp = qf(key, currentTime, currentTick)
		du.recentDedup[args.DeduplicationID] = dedupState{amount: amount, exp: t}
	}

	du.Unlock()

	if dup {
		du.logger.Infof("Quota operation satisfied through deduplication: dedupID %v, amount %v", args.DeduplicationID, result.amount)
	}

	return amount, exp, key, nil
}

// reapDedup cleans up dedup entries from the oldDedup map and moves all entries from
// the recentDedup map into the oldDedup map, making those next in line for deletion.
//
// This is normally called on a regular basis via a go routine. It's also used directly
// from tests to inject specific behaviors.
func (du *dedupUtil) reapDedup() {
	t := du.oldDedup
	du.oldDedup = du.recentDedup
	du.recentDedup = t

	if len(t) > 0 && du.logger.VerbosityLevel(4) {
		du.logger.Infof("Running repear to reclaim %d old deduplication entries", len(t))
	}

	// TODO: why isn't there a O(1) way to clear a map to the empty state?!
	for k := range t {
		delete(t, k)
	}
}
