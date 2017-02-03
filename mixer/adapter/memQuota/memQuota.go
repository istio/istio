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

	"istio.io/mixer/adapter/memQuota/config"
	"istio.io/mixer/pkg/adapter"
)

type builder struct{ adapter.DefaultBuilder }

type memQuota struct {
	sync.Mutex

	// the definitions we know about, immutable
	definitions map[string]*adapter.QuotaDefinition

	// the counters we track for non-expiring quotas, protected by lock
	cells map[string]int64

	// the rolling windows we track for expiring quotas, protected by lock
	windows map[string]*rollingWindow

	// two ping-ponging maps of active dedup ids
	recentDedup map[string]int64
	oldDedup    map[string]int64

	// used for reaping dedup ids
	ticker *time.Ticker

	// indirection to support fast deterministic tests
	getTick func() int32

	logger adapter.Logger
}

// we maintain a pool of these for use by the makeKey function
type keyWorkspace struct {
	keys   []string
	buffer []byte
}

// pool of reusable keyWorkspace structs
var keyWorkspacePool = sync.Pool{New: func() interface{} { return &keyWorkspace{} }}

var (
	name = "memQuota"
	desc = "Simple volatile memory-based quotas."
	conf = &config.Params{MinDeduplicationWindowSeconds: 1}
)

const (
	// See the rollingWindow comment for a description of what this is for.
	ticksPerSecond = 10
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

	if c.MinDeduplicationWindowSeconds <= 0 {
		ce = ce.Appendf("MinDeduplicationWindowSeconds", "deduplication window of %d is invalid, must be >= 0", c.MinDeduplicationWindowSeconds)
	}

	return
}

func (builder) NewQuotasAspect(env adapter.Env, c adapter.AspectConfig, d map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return newAspect(env, c.(*config.Params), d)
}

// newAspect returns a new aspect.
func newAspect(env adapter.Env, c *config.Params, definitions map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return newAspectWithDedup(env, time.NewTicker(time.Duration(c.MinDeduplicationWindowSeconds)*time.Second), definitions)
}

// newAspect returns a new aspect.
func newAspectWithDedup(env adapter.Env, ticker *time.Ticker, definitions map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	mq := &memQuota{
		definitions: definitions,
		cells:       make(map[string]int64),
		windows:     make(map[string]*rollingWindow),
		recentDedup: make(map[string]int64),
		oldDedup:    make(map[string]int64),
		ticker:      ticker,
		getTick:     func() int32 { return int32(time.Now().UnixNano() / (1000000000 / ticksPerSecond)) },
		logger:      env.Logger(),
	}

	go func() {
		for range mq.ticker.C {
			mq.Lock()
			mq.reapDedup()
			mq.Unlock()
		}
	}()

	return mq, nil
}

func (mq *memQuota) Close() error {
	mq.ticker.Stop()
	return nil
}

func (mq *memQuota) Alloc(args adapter.QuotaArgs) (int64, error) {
	return mq.alloc(args, false)
}

func (mq *memQuota) AllocBestEffort(args adapter.QuotaArgs) (int64, error) {
	return mq.alloc(args, true)
}

func (mq *memQuota) alloc(args adapter.QuotaArgs, bestEffort bool) (int64, error) {
	return mq.commonWrapper(args, func(d *adapter.QuotaDefinition, key string) int64 {
		result := args.QuotaAmount

		// we optimize storage for non-expiring quotas
		if d.Window == 0 {
			inUse := mq.cells[key]

			if result > d.MaxAmount-inUse {
				if !bestEffort {
					return 0
				}

				// grab as much as we can
				result = d.MaxAmount - inUse
			}
			mq.cells[key] = inUse + result
			return result
		}

		window, ok := mq.windows[key]
		if !ok {
			seconds := int32((d.Window + time.Second - 1) / time.Second)
			window = newRollingWindow(d.MaxAmount, seconds*ticksPerSecond)
			mq.windows[key] = window
		}

		currentTick := mq.getTick()
		if !window.alloc(result, currentTick) {
			if !bestEffort {
				return 0
			}

			// grab as much as we can
			result = window.available()
			_ = window.alloc(result, currentTick)
		}

		return result
	})
}

func (mq *memQuota) ReleaseBestEffort(args adapter.QuotaArgs) (int64, error) {
	return mq.commonWrapper(args, func(d *adapter.QuotaDefinition, key string) int64 {
		result := args.QuotaAmount

		if d.Window == 0 {
			inUse := mq.cells[key]

			if result >= inUse {
				// delete the cell since it contains no useful state
				delete(mq.cells, key)
				return inUse
			}

			mq.cells[key] = inUse - result
			return result
		}

		window, ok := mq.windows[key]
		if !ok {
			return 0
		}

		currentTick := mq.getTick()
		result = window.release(result, currentTick)

		if window.available() == d.MaxAmount {
			// delete the cell since it contains no useful state
			delete(mq.windows, key)
		}

		return result
	})
}

type quotaFunc func(d *adapter.QuotaDefinition, key string) int64

func (mq *memQuota) commonWrapper(args adapter.QuotaArgs, qf quotaFunc) (int64, error) {
	d, ok := mq.definitions[args.Name]
	if !ok {
		return 0, fmt.Errorf("request for unknown quota '%s' received", args.Name)
	}

	if args.QuotaAmount < 0 {
		return 0, fmt.Errorf("negative quota amount %d received", args.QuotaAmount)
	}

	if args.QuotaAmount == 0 {
		return 0, nil
	}

	key := makeKey(args)

	mq.Lock()

	result, dup := mq.recentDedup[args.DeduplicationID]
	if !dup {
		result, dup = mq.oldDedup[args.DeduplicationID]
	}

	if dup {
		mq.logger.Infof("Quota operation satisfied through deduplication: dedupID %v, amount %v", args.DeduplicationID, result)
	} else {
		result = qf(d, key)
		mq.recentDedup[args.DeduplicationID] = result
	}

	mq.Unlock()

	return result, nil
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

	// TODO: why isn't there a O(1) way to clear a map to the empty state?!
	for k := range t {
		delete(t, k)
	}
}

// Produce a unique key representing the cell
func makeKey(args adapter.QuotaArgs) string {
	ws := keyWorkspacePool.Get().(*keyWorkspace)
	keys := ws.keys
	buffer := ws.buffer

	// get all the label names
	for k := range args.Labels {
		keys = append(keys, k)
	}

	// ensure stable order
	sort.Strings(keys)

	buffer = append(buffer, []byte(args.Name)...)
	for _, k := range keys {
		buffer = append(buffer, []byte(";")...)
		buffer = append(buffer, []byte(k)...)
		buffer = append(buffer, []byte("=")...)

		switch v := args.Labels[k].(type) {
		case string:
			buffer = append(buffer, []byte(v)...)
		case int64:
			buffer = strconv.AppendInt(buffer, v, 16)
		case float64:
			buffer = strconv.AppendFloat(buffer, v, 'b', -1, 64)
		case bool:
			buffer = strconv.AppendBool(buffer, v)
		case []byte:
			buffer = append(buffer, v...)
		default:
			buffer = append(buffer, []byte(v.(fmt.Stringer).String())...)
		}
	}

	result := string(buffer)

	ws.keys = keys[:0]
	ws.buffer = buffer[:0]
	keyWorkspacePool.Put(ws)

	return result
}
