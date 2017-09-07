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

// Package memquota provides a simple in-memory quota implementation. It's
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
package memquota // import "istio.io/mixer/adapter/memquota"

import (
	"context"
	"fmt"
	"time"

	"istio.io/mixer/adapter/memquota2/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/status"
	"istio.io/mixer/template/quota"
)

type handler struct {
	// common info among different quota adapters
	common dedupUtil

	// the counters we track for non-expiring quotas, protected by lock
	cells map[string]int64

	// the rolling windows we track for expiring quotas, protected by lock
	windows map[string]*rollingWindow

	// the limits we know about
	limits map[string]config.Params_Quota
}

func (h *handler) HandleQuota(context context.Context, instance *quota.Instance, args adapter.QuotaRequestArgs) (adapter.QuotaResult2, error) {
	q := h.limits[instance.Name]

	if args.QuotaAmount > 0 {
		return h.alloc(instance, args, q)
	} else if args.QuotaAmount < 0 {
		args.QuotaAmount = -args.QuotaAmount
		return h.free(instance, args, q)
	}
	return adapter.QuotaResult2{}, nil
}

func (h *handler) alloc(instance *quota.Instance, args adapter.QuotaRequestArgs, q config.Params_Quota) (adapter.QuotaResult2, error) {
	amount, exp, err := h.common.handleDedup(instance, args, func(key string, currentTime time.Time, currentTick int64) (int64, time.Time,
		time.Duration) {
		result := args.QuotaAmount

		// we optimize storage for non-expiring quotas
		if q.ValidDuration == 0 {
			inUse := h.cells[key]

			if result > q.MaxAmount-inUse {
				if !args.BestEffort {
					return 0, time.Time{}, 0
				}

				// grab as much as we can
				result = q.MaxAmount - inUse
			}
			h.cells[key] = inUse + result
			return result, time.Time{}, 0
		}

		window, ok := h.windows[key]
		if !ok {
			seconds := int32((q.ValidDuration + time.Second - 1) / time.Second)
			window = newRollingWindow(q.MaxAmount, int64(seconds)*ticksPerSecond)
			h.windows[key] = window
		}

		if !window.alloc(result, currentTick) {
			if !args.BestEffort {
				return 0, time.Time{}, 0
			}

			// grab as much as we can
			result = window.available()
			_ = window.alloc(result, currentTick)
		}

		return result, currentTime.Add(q.ValidDuration), q.ValidDuration
	})

	return adapter.QuotaResult2{
		Status:        status.OK,
		Amount:        amount,
		ValidDuration: exp,
	}, err
}

func (h *handler) free(instance *quota.Instance, args adapter.QuotaRequestArgs, q config.Params_Quota) (adapter.QuotaResult2, error) {
	amount, _, err := h.common.handleDedup(instance, args, func(key string, currentTime time.Time, currentTick int64) (int64, time.Time,
		time.Duration) {
		result := args.QuotaAmount

		if q.ValidDuration == 0 {
			inUse := h.cells[key]

			if result >= inUse {
				// delete the cell since it contains no useful state
				delete(h.cells, key)
				return inUse, time.Time{}, 0
			}

			h.cells[key] = inUse - result
			return result, time.Time{}, 0
		}

		// WARNING: Releasing quota in the case of rate limits is
		//          inherently racy. A release can easily end up
		//          freeing quota in the wrong window.

		window, ok := h.windows[key]
		if !ok {
			return 0, time.Time{}, 0
		}

		result = window.release(result, currentTick)

		if window.available() == q.MaxAmount {
			// delete the cell since it contains no useful state
			delete(h.windows, key)
		}

		return result, time.Time{}, 0
	})

	return adapter.QuotaResult2{
		Status: status.OK,
		Amount: amount,
	}, err
}

func (h *handler) Close() error {
	h.common.ticker.Stop()
	return nil
}

////////////////// Config //////////////////////////

// GetInfo returns the BuilderInfo associated with this adapter implementation.
func GetInfo() adapter.BuilderInfo {
	return adapter.BuilderInfo{
		Name:        "memquota",
		Impl:        "istio.io/mixer/adapter/memquota",
		Description: "Volatile memory-based quota tracking",
		SupportedTemplates: []string{
			quota.TemplateName,
		},
		DefaultConfig: &config.Params{
			MinDeduplicationDuration: 1 * time.Second,
		},

		NewBuilder: func() adapter.HandlerBuilder { return &builder{} },
	}
}

type builder struct {
	adapterConfig *config.Params
	quotaTypes    map[string]*quota.Type
}

func (b *builder) SetQuotaTypes(types map[string]*quota.Type) { b.quotaTypes = types }
func (b *builder) SetAdapterConfig(cfg adapter.Config)        { b.adapterConfig = cfg.(*config.Params) }

func (b *builder) Validate() (ce *adapter.ConfigErrors) {
	ac := b.adapterConfig

	if ac.MinDeduplicationDuration <= 0 {
		ce = ce.Appendf("minDeduplicationDuration", "deduplication window of %v is invalid, must be > 0", ac.MinDeduplicationDuration)
	}
	return
}

func (b *builder) Build(context context.Context, env adapter.Env) (adapter.Handler, error) {
	ac := b.adapterConfig
	return b.buildWithDedup(context, env, time.NewTicker(ac.MinDeduplicationDuration))
}

func (b *builder) buildWithDedup(_ context.Context, env adapter.Env, ticker *time.Ticker) (*handler, error) {
	ac := b.adapterConfig

	limits := make(map[string]config.Params_Quota, len(ac.Quotas))
	for _, l := range ac.Quotas {
		limits[l.Name] = l
	}

	for k := range b.quotaTypes {
		if _, ok := limits[k]; !ok {
			return nil, fmt.Errorf("did not find limit defined for quota %s", k)
		}
	}

	h := &handler{
		common: dedupUtil{
			recentDedup: make(map[string]dedupState),
			oldDedup:    make(map[string]dedupState),
			ticker:      ticker,
			getTime:     time.Now,
			logger:      env.Logger(),
		},
		cells:   make(map[string]int64),
		windows: make(map[string]*rollingWindow),
		limits:  limits,
	}

	env.ScheduleDaemon(func() {
		for range h.common.ticker.C {
			h.common.Lock()
			h.common.reapDedup()
			h.common.Unlock()
		}
	})

	return h, nil
}
