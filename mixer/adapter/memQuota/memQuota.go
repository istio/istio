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

// Package memQuota provides a simple in-memory quota implementation. It's
// trivial to set up, but it has various limitations:
//
// - Obviously, the data set must be able to fit in memory.
//
// - When Mixer crashes/restarts, all quota values are erased.
// This means this isn't good for allocation quotas although
// it works well enough for rate limits quotas.
//
// - Since the data is all memory-resident and there isn't any cross-node
// synchronization, this adapter can't be used in an Istio mixer where
// a single service can be handled by different mixer instances.
package memQuota

import (
	"time"

	"istio.io/mixer/adapter/memQuota/config"
	"istio.io/mixer/adapter/memQuota/util"
	"istio.io/mixer/pkg/adapter"
)

type builder struct{ adapter.DefaultBuilder }

type memQuota struct {
	// common info among different quota adapters
	common util.QuotaUtil

	// the counters we track for non-expiring quotas, protected by lock
	cells map[string]int64

	// the rolling windows we track for expiring quotas, protected by lock
	windows map[string]*rollingWindow
}

var (
	name = "memQuota"
	desc = "Simple volatile memory-based quotas."
	conf = &config.Params{
		MinDeduplicationDuration: 1 * time.Second,
	}
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterQuotasBuilder(newBuilder())
}

func newBuilder() builder {
	return builder{adapter.NewDefaultBuilder(name, desc, conf)}
}

func (builder) ValidateConfig(cfg adapter.Config) (ce *adapter.ConfigErrors) {
	c := cfg.(*config.Params)

	if c.MinDeduplicationDuration <= 0 {
		ce = ce.Appendf("minDeduplicationDuration", "deduplication window of %v is invalid, must be > 0", c.MinDeduplicationDuration)
	}
	return
}

func (builder) NewQuotasAspect(env adapter.Env, c adapter.Config, _ map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return newAspect(env, c.(*config.Params))
}

// newAspect returns a new aspect.
func newAspect(env adapter.Env, c *config.Params) (adapter.QuotasAspect, error) {
	return newAspectWithDedup(env, time.NewTicker(c.MinDeduplicationDuration))
}

// newAspect returns a new aspect.
func newAspectWithDedup(env adapter.Env, ticker *time.Ticker) (adapter.QuotasAspect, error) {
	mq := &memQuota{
		common: util.QuotaUtil{
			RecentDedup: make(map[string]util.DedupState),
			OldDedup:    make(map[string]util.DedupState),
			Ticker:      ticker,
			GetTime:     time.Now,
			Logger:      env.Logger(),
		},
		cells:   make(map[string]int64),
		windows: make(map[string]*rollingWindow),
	}

	env.ScheduleDaemon(func() {
		for range mq.common.Ticker.C {
			mq.common.Lock()
			mq.common.ReapDedup()
			mq.common.Unlock()
		}
	})

	return mq, nil
}

func (mq *memQuota) Close() error {
	mq.common.Ticker.Stop()
	return nil
}

func (mq *memQuota) Alloc(args adapter.QuotaArgsLegacy) (adapter.QuotaResultLegacy, error) {
	return mq.alloc(args, false)
}

func (mq *memQuota) AllocBestEffort(args adapter.QuotaArgsLegacy) (adapter.QuotaResultLegacy, error) {
	return mq.alloc(args, true)
}

func (mq *memQuota) alloc(args adapter.QuotaArgsLegacy, bestEffort bool) (adapter.QuotaResultLegacy, error) {
	amount, exp, err := mq.common.CommonWrapper(args, func(d *adapter.QuotaDefinition, key string, currentTime time.Time, currentTick int64) (int64, time.Time,
		time.Duration) {
		result := args.QuotaAmount

		// we optimize storage for non-expiring quotas
		if d.Expiration == 0 {
			inUse := mq.cells[key]

			if result > d.MaxAmount-inUse {
				if !bestEffort {
					return 0, time.Time{}, 0
				}

				// grab as much as we can
				result = d.MaxAmount - inUse
			}
			mq.cells[key] = inUse + result
			return result, time.Time{}, 0
		}

		window, ok := mq.windows[key]
		if !ok {
			seconds := int32((d.Expiration + time.Second - 1) / time.Second)
			window = newRollingWindow(d.MaxAmount, int64(seconds)*util.TicksPerSecond)
			mq.windows[key] = window
		}

		if !window.alloc(result, currentTick) {
			if !bestEffort {
				return 0, time.Time{}, 0
			}

			// grab as much as we can
			result = window.available()
			_ = window.alloc(result, currentTick)
		}

		return result, currentTime.Add(d.Expiration), d.Expiration
	})

	return adapter.QuotaResultLegacy{
		Amount:     amount,
		Expiration: exp,
	}, err
}

func (mq *memQuota) ReleaseBestEffort(args adapter.QuotaArgsLegacy) (int64, error) {
	amount, _, err := mq.common.CommonWrapper(args,
		func(d *adapter.QuotaDefinition, key string, currentTime time.Time, currentTick int64) (int64, time.Time, time.Duration) {
			result := args.QuotaAmount

			if d.Expiration == 0 {
				inUse := mq.cells[key]

				if result >= inUse {
					// delete the cell since it contains no useful state
					delete(mq.cells, key)
					return inUse, time.Time{}, 0
				}

				mq.cells[key] = inUse - result
				return result, time.Time{}, 0
			}

			// WARNING: Releasing quota in the case of rate limits is
			//          inherently racy. A release can easily end up
			//          freeing quota in the wrong window.

			window, ok := mq.windows[key]
			if !ok {
				return 0, time.Time{}, 0
			}

			result = window.release(result, currentTick)

			if window.available() == d.MaxAmount {
				// delete the cell since it contains no useful state
				delete(mq.windows, key)
			}

			return result, time.Time{}, 0
		})

	return amount, err
}
