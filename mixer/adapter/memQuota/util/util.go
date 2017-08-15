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

package util

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/pool"
)

// QuotaUtil stores common information for in-memory and redis quota adapters
type QuotaUtil struct {
	sync.Mutex

	// two ping-ponging maps of active dedup ids
	RecentDedup map[string]DedupState
	OldDedup    map[string]DedupState

	// used for reaping dedup ids
	Ticker *time.Ticker

	// indirection to support fast deterministic tests
	GetTime func() time.Time

	Logger adapter.Logger
}

// DedupState maintains a dedup state
type DedupState struct {
	amount int64
	exp    time.Time
}

// we maintain a pool of these for use by the makeKey function
type keyWorkspace struct {
	keys []string
}

const (
	// TicksPerSecond determines the number of quantized time intervals in 1 second.
	TicksPerSecond = 10

	// NanosPerTick is equal to ns/tick
	NanosPerTick = int64(time.Second / TicksPerSecond)
)

// pool of reusable keyWorkspace structs
var keyWorkspacePool = sync.Pool{New: func() interface{} { return &keyWorkspace{} }}

type quotaFunc func(d *adapter.QuotaDefinition, key string, currentTime time.Time, currentTick int64) (int64, time.Time, time.Duration)

// CommonWrapper is a wrapper function to generate useful quota info.
func (qu *QuotaUtil) CommonWrapper(args adapter.QuotaArgs, qf quotaFunc) (int64, time.Duration, error) {
	d := args.Definition
	if args.QuotaAmount < 0 {
		return 0, 0, fmt.Errorf("negative quota amount %d received", args.QuotaAmount)
	}

	if args.QuotaAmount == 0 {
		return 0, 0, nil
	}

	key := MakeKey(args.Definition.Name, args.Labels)

	qu.Lock()

	currentTime := qu.GetTime()
	currentTick := currentTime.UnixNano() / NanosPerTick

	var amount int64
	var t time.Time
	var exp time.Duration

	result, dup := qu.RecentDedup[args.DeduplicationID]
	if !dup {
		result, dup = qu.OldDedup[args.DeduplicationID]
	}

	if dup {
		qu.Logger.Infof("Quota operation satisfied through deduplication: dedupID %v, amount %v", args.DeduplicationID, result.amount)
		amount = result.amount
		exp = result.exp.Sub(currentTime)
		if exp < 0 {
			exp = 0
		}
	} else {
		amount, t, exp = qf(d, key, currentTime, currentTick)
		qu.RecentDedup[args.DeduplicationID] = DedupState{amount: amount, exp: t}
	}

	qu.Unlock()

	return amount, exp, nil
}

// ReapDedup cleans up dedup entries from the oldDedup map and moves all entries from
// the recentDedup map into the oldDedup map, making those next in line for deletion.
//
// This is normally called on a regular basis via a go routine. It's also used directly
// from tests to inject specific behaviors.
func (qu *QuotaUtil) ReapDedup() {
	t := qu.OldDedup
	qu.OldDedup = qu.RecentDedup
	qu.RecentDedup = t

	if qu.Logger.VerbosityLevel(4) {
		qu.Logger.Infof("Running repear to reclaim %d old deduplication entries", len(t))
	}

	// TODO: why isn't there a O(1) way to clear a map to the empty state?!
	for k := range t {
		delete(t, k)
	}
}

// MakeKey produces a unique key representing the given labels.
func MakeKey(name string, labels map[string]interface{}) string {
	ws := keyWorkspacePool.Get().(*keyWorkspace)
	keys := ws.keys
	buf := pool.GetBuffer()

	// ensure stable order
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf.WriteString(name) // nolint: gas
	for _, k := range keys {
		buf.WriteString(";") // nolint: gas
		buf.WriteString(k)   // nolint: gas
		buf.WriteString("=") // nolint: gas

		switch v := labels[k].(type) {
		case string:
			buf.WriteString(v) // nolint: gas
		case int64:
			var bytes [32]byte
			buf.Write(strconv.AppendInt(bytes[:], v, 16)) // nolint: gas
		case float64:
			var bytes [32]byte
			buf.Write(strconv.AppendFloat(bytes[:], v, 'b', -1, 64)) // nolint: gas
		case bool:
			var bytes [32]byte
			buf.Write(strconv.AppendBool(bytes[:], v)) // nolint: gas
		case []byte:
			buf.Write(v) // nolint: gas
		case map[string]string:
			ws := keyWorkspacePool.Get().(*keyWorkspace)
			mk := ws.keys

			// ensure stable order
			for k2 := range v {
				mk = append(mk, k2)
			}
			sort.Strings(mk)

			for _, k2 := range mk {
				buf.WriteString(k2)    // nolint: gas
				buf.WriteString(v[k2]) // nolint: gas
			}

			ws.keys = keys[:0]
			keyWorkspacePool.Put(ws)
		default:
			buf.WriteString(v.(fmt.Stringer).String()) // nolint: gas
		}
	}

	result := buf.String()
	pool.PutBuffer(buf)

	ws.keys = keys[:0]
	keyWorkspacePool.Put(ws)

	return result
}
