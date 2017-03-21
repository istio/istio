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
// - When the mixer crashes/restarts, all quota values are erased.
// This means this isn't good for allocation quotas although
// it works well enough for rate limits quotas.
//
// - Since the data is all memory-resident and there isn't any cross-node
// synchronization, this adapter can't be used in an Istio mixer where
// a single service can be handled by different mixer instances.
package memQuota

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	ptypes "github.com/gogo/protobuf/types"

	"istio.io/mixer/adapter/memQuota/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/pool"
)

type builder struct{ adapter.DefaultBuilder }

type memQuota struct {
	sync.Mutex

	// the counters we track for non-expiring quotas, protected by lock
	cells map[string]int64

	// the rolling windows we track for expiring quotas, protected by lock
	windows map[string]*rollingWindow

	// two ping-ponging maps of active dedup ids
	recentDedup map[string]dedupState
	oldDedup    map[string]dedupState

	// used for reaping dedup ids
	ticker *time.Ticker

	// indirection to support fast deterministic tests
	getTime func() time.Time

	logger adapter.Logger
}

type dedupState struct {
	amount int64
	exp    time.Time
}

// we maintain a pool of these for use by the makeKey function
type keyWorkspace struct {
	keys []string
}

// pool of reusable keyWorkspace structs
var keyWorkspacePool = sync.Pool{New: func() interface{} { return &keyWorkspace{} }}

var (
	name = "memQuota"
	desc = "Simple volatile memory-based quotas."
	conf = &config.Params{
		MinDeduplicationDuration: &ptypes.Duration{Seconds: 1},
	}
)

const (
	// See the rollingWindow comment for a description of what this is for.
	ticksPerSecond = 10

	// ns/tick
	nanosPerTick = int64(time.Second / ticksPerSecond)
)

// Register records the builders exposed by this adapter.
func Register(r adapter.Registrar) {
	r.RegisterQuotasBuilder(newBuilder())
}

func newBuilder() builder {
	return builder{adapter.NewDefaultBuilder(name, desc, conf)}
}

func (builder) ValidateConfig(cfg adapter.AspectConfig) (ce *adapter.ConfigErrors) {
	c := cfg.(*config.Params)

	dedupWindow, err := ptypes.DurationFromProto(c.MinDeduplicationDuration)
	if err != nil {
		ce = ce.Append("MinDeduplicationDuration", err)
		return
	}
	if dedupWindow <= 0 {
		ce = ce.Appendf("MinDeduplicationDuration", "deduplication window of %v is invalid, must be > 0", dedupWindow)
	}
	return
}

func (builder) NewQuotasAspect(env adapter.Env, c adapter.AspectConfig, d map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return newAspect(env, c.(*config.Params))
}

// newAspect returns a new aspect.
func newAspect(env adapter.Env, c *config.Params) (adapter.QuotasAspect, error) {
	dedupWindow, _ := ptypes.DurationFromProto(c.MinDeduplicationDuration)

	return newAspectWithDedup(env, time.NewTicker(dedupWindow))
}

// newAspect returns a new aspect.
func newAspectWithDedup(env adapter.Env, ticker *time.Ticker) (adapter.QuotasAspect, error) {
	mq := &memQuota{
		cells:       make(map[string]int64),
		windows:     make(map[string]*rollingWindow),
		recentDedup: make(map[string]dedupState),
		oldDedup:    make(map[string]dedupState),
		ticker:      ticker,
		getTime:     time.Now,
		logger:      env.Logger(),
	}

	env.ScheduleDaemon(func() {
		for range mq.ticker.C {
			mq.Lock()
			mq.reapDedup()
			mq.Unlock()
		}
	})

	return mq, nil
}

func (mq *memQuota) Close() error {
	mq.ticker.Stop()
	return nil
}

func (mq *memQuota) Alloc(args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return mq.alloc(args, false)
}

func (mq *memQuota) AllocBestEffort(args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return mq.alloc(args, true)
}

func (mq *memQuota) alloc(args adapter.QuotaArgs, bestEffort bool) (adapter.QuotaResult, error) {
	amount, exp, err := mq.commonWrapper(args, func(d *adapter.QuotaDefinition, key string, currentTime time.Time, currentTick int64) (int64, time.Time,
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
			window = newRollingWindow(d.MaxAmount, int64(seconds)*ticksPerSecond)
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

	return adapter.QuotaResult{
		Amount:     amount,
		Expiration: exp,
	}, err
}

func (mq *memQuota) ReleaseBestEffort(args adapter.QuotaArgs) (int64, error) {
	amount, _, err := mq.commonWrapper(args,
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

type quotaFunc func(d *adapter.QuotaDefinition, key string, currentTime time.Time, currentTick int64) (int64, time.Time, time.Duration)

func (mq *memQuota) commonWrapper(args adapter.QuotaArgs, qf quotaFunc) (int64, time.Duration, error) {
	d := args.Definition
	if args.QuotaAmount < 0 {
		return 0, 0, fmt.Errorf("negative quota amount %d received", args.QuotaAmount)
	}

	if args.QuotaAmount == 0 {
		return 0, 0, nil
	}

	key := makeKey(args.Definition.Name, args.Labels)

	mq.Lock()

	currentTime := mq.getTime()
	currentTick := currentTime.UnixNano() / nanosPerTick

	var amount int64
	var t time.Time
	var exp time.Duration

	result, dup := mq.recentDedup[args.DeduplicationID]
	if !dup {
		result, dup = mq.oldDedup[args.DeduplicationID]
	}

	if dup {
		mq.logger.Infof("Quota operation satisfied through deduplication: dedupID %v, amount %v", args.DeduplicationID, result.amount)
		amount = result.amount
		exp = result.exp.Sub(currentTime)
		if exp < 0 {
			exp = 0
		}
	} else {
		amount, t, exp = qf(d, key, currentTime, currentTick)
		mq.recentDedup[args.DeduplicationID] = dedupState{amount: amount, exp: t}
	}

	mq.Unlock()

	return amount, exp, nil
}

// reapDedup cleans up dedup entries from the oldDedup map and moves all entries from
// the recentDedup map into the oldDedup map, making those next in line for deletion.
//
// This is normally called on a regular basis via a go routine. It's also used directly
// from tests to inject specific behaviors.
func (mq *memQuota) reapDedup() {
	t := mq.oldDedup
	mq.oldDedup = mq.recentDedup
	mq.recentDedup = t

	mq.logger.Infof("Running repear to reclaim %d old deduplication entries", len(t))

	// TODO: why isn't there a O(1) way to clear a map to the empty state?!
	for k := range t {
		delete(t, k)
	}
}

// Produce a unique key representing the given labels.
func makeKey(name string, labels map[string]interface{}) string {
	ws := keyWorkspacePool.Get().(*keyWorkspace)
	keys := ws.keys
	buf := pool.GetBuffer()

	// ensure stable order
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf.WriteString(name)
	for _, k := range keys {
		buf.WriteString(";")
		buf.WriteString(k)
		buf.WriteString("=")

		switch v := labels[k].(type) {
		case string:
			buf.WriteString(v)
		case int64:
			var bytes [32]byte
			buf.Write(strconv.AppendInt(bytes[:], v, 16))
		case float64:
			var bytes [32]byte
			buf.Write(strconv.AppendFloat(bytes[:], v, 'b', -1, 64))
		case bool:
			var bytes [32]byte
			buf.Write(strconv.AppendBool(bytes[:], v))
		case []byte:
			buf.Write(v)
		case map[string]string:
			ws := keyWorkspacePool.Get().(*keyWorkspace)
			mk := ws.keys

			// ensure stable order
			for k2 := range v {
				mk = append(mk, k2)
			}
			sort.Strings(mk)

			for _, k2 := range mk {
				buf.WriteString(k2)
				buf.WriteString(v[k2])
			}

			ws.keys = keys[:0]
			keyWorkspacePool.Put(ws)
		default:
			buf.WriteString(v.(fmt.Stringer).String())
		}
	}

	result := buf.String()

	pool.PutBuffer(buf)
	ws.keys = keys[:0]
	keyWorkspacePool.Put(ws)

	return result
}
