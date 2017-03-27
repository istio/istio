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

// Package redisquota provides a quota implementation with redis as backend.
// The prerequisite is to have a redis server running.
package redisquota

import (
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	ptypes "github.com/gogo/protobuf/types"

	"istio.io/mixer/adapter/redisquota/config"
	"istio.io/mixer/pkg/adapter"
	"istio.io/mixer/pkg/pool"
)

type builder struct{ adapter.DefaultBuilder }

type redisQuota struct {
	sync.Mutex

	// the definitions we know about, immutable
	definitions map[string]*adapter.QuotaDefinition

	// the counters we track for non-expiring quotas, protected by lock
	cells map[string]int64

	// connection pool with redis
	redisPool *connPool

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
	name = "redisQuota"
	desc = "Redis-based quotas."
	conf = &config.Params{
		MinDeduplicationDuration: &ptypes.Duration{Seconds: 1},
		RedisServerUrl:           "localhost:6379",
		SocketType:               "tcp",
		ConnectionPoolSize:       10,
	}
)

const (
	// Time is abstracted in terms of ticks, provided by the caller, decoupling the
	// implementation from real-time, enabling much easier testing and more flexibility.
	ticksPerSecond = 10

	// ns/tick
	nanosPerTick = int64(time.Second / ticksPerSecond)
)

// Register records the builders exposed by this adapter.
// TODO: need to be registered in inventory
func Register(r adapter.Registrar) {
	r.RegisterQuotasBuilder(newBuilder())
}

func newBuilder() builder {
	return builder{adapter.NewDefaultBuilder(name, desc, conf)}
}

func (builder) ValidateConfig(cfg adapter.Config) (ce *adapter.ConfigErrors) {
	c := cfg.(*config.Params)

	dedupWindow, err := ptypes.DurationFromProto(c.MinDeduplicationDuration)
	if err != nil {
		ce = ce.Append("MinDeduplicationDuration", err)
		return
	}
	if dedupWindow <= 0 {
		ce = ce.Appendf("MinDeduplicationDuration", "deduplication window of %v is invalid, must be > 0", dedupWindow)
	}

	if c.ConnectionPoolSize < 0 {
		ce = ce.Appendf("ConnectionPoolSize", "redis connection pool size of %v is invalid, must be > 0", c.ConnectionPoolSize)

	}

	return
}

func (builder) NewQuotasAspect(env adapter.Env, c adapter.Config, d map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return newAspect(env, c.(*config.Params), d)
}

// newAspect returns a new aspect.
func newAspect(env adapter.Env, c *config.Params, definitions map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	dedupWindow, _ := ptypes.DurationFromProto(c.MinDeduplicationDuration)

	return newAspectWithDedup(env, time.NewTicker(dedupWindow), c, definitions)
}

// newAspectWithDedup returns a new aspect.
func newAspectWithDedup(env adapter.Env, ticker *time.Ticker, c *config.Params, definitions map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	connPool, err := newConnPool(c.RedisServerUrl, c.SocketType, c.ConnectionPoolSize)
	if err != nil {
		// TODO: propagate Connection Pool error here
		return nil, err
	}
	rq := &redisQuota{
		definitions: definitions,
		cells:       make(map[string]int64),
		redisPool:   connPool,
		recentDedup: make(map[string]dedupState),
		oldDedup:    make(map[string]dedupState),
		ticker:      ticker,
		logger:      env.Logger(),
	}
	return rq, nil
}

func (rq *redisQuota) Alloc(args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return rq.alloc(args, false)
}

func (rq *redisQuota) AllocBestEffort(args adapter.QuotaArgs) (adapter.QuotaResult, error) {
	return rq.alloc(args, true)
}

func (rq *redisQuota) alloc(args adapter.QuotaArgs, bestEffort bool) (adapter.QuotaResult, error) {
	amount, exp, err := rq.commonWrapper(args, func(d *adapter.QuotaDefinition, key string, currentTime time.Time, currentTick int64) (int64, time.Time,
		time.Duration) {
		result := args.QuotaAmount
		conn, _ := rq.redisPool.get()
		// TODO: propagate Connection Pool error here
		// if err != nil {
		// 	rq.logger.Infof("Could not get connection to redis")
		// 	return 0, time.Time{}, 0
		//}
		defer rq.redisPool.put(conn)

		// increase the value of this key by the amount of result
		conn.pipeAppend("INCRBY", key, result)
		resp, _ := conn.pipeResponse()
		// TODO: propagate Connection Pool error here
		// if err != nil {
		//	rq.logger.Infof("Could not get response from redis")
		//	return 0, time.Time{}, 0
		//}
		ret := resp.int()

		if ret > d.MaxAmount {
			if !bestEffort {
				return 0, time.Time{}, 0
			}
			// grab as much as we can
			result = d.MaxAmount - (ret - result)
		}

		return result, currentTime.Add(d.Expiration), d.Expiration
	})

	return adapter.QuotaResult{
		Amount:     amount,
		Expiration: exp,
	}, err

}

func (rq *redisQuota) ReleaseBestEffort(args adapter.QuotaArgs) (int64, error) {
	amount, _, err := rq.commonWrapper(args,
		func(d *adapter.QuotaDefinition, key string, currentTime time.Time, currentTick int64) (int64, time.Time, time.Duration) {
			result := args.QuotaAmount
			conn, _ := rq.redisPool.get()
			// TODO: propagate Connection Pool error here
			// if err != nil {
			// 	return 0, time.Time{}, 0
			// }
			defer rq.redisPool.put(conn)

			// decrease the value of this key by the amount of result
			conn.pipeAppend("DECRBY", key, result)
			resp, _ := conn.pipeResponse()
			// TODO: propagate Connection Pool error here
			// TODO: propagate Connection Pool error here
			// if err != nil {
			// 	return 0, time.Time{}, 0
			// }
			ret := resp.int()

			if ret <= 0 {
				// delete the key since it contains no useful state
				conn.pipeAppend("DEL", key)
				// consume the output of previous command
				resp, _ = conn.pipeResponse()
			}

			return result, time.Time{}, 0
		})

	return amount, err
}

func (rq *redisQuota) Close() error {
	rq.redisPool.empty()
	return nil
}

type quotaFunc func(d *adapter.QuotaDefinition, key string, currentTime time.Time, currentTick int64) (int64, time.Time, time.Duration)

// TODO: extract the common code below between memQuota and redisQuota into a util-like package, need to figure out where to put the package?
func (rq *redisQuota) commonWrapper(args adapter.QuotaArgs, qf quotaFunc) (int64, time.Duration, error) {
	d := args.Definition
	if args.QuotaAmount < 0 {
		return 0, 0, fmt.Errorf("negative quota amount %d received", args.QuotaAmount)
	}

	if args.QuotaAmount == 0 {
		return 0, 0, nil
	}

	key := makeKey(args.Definition.Name, args.Labels)

	rq.Lock()

	currentTime := rq.getTime()
	currentTick := currentTime.UnixNano() / nanosPerTick

	var amount int64
	var t time.Time
	var exp time.Duration

	result, dup := rq.recentDedup[args.DeduplicationID]
	if !dup {
		result, dup = rq.oldDedup[args.DeduplicationID]
	}

	if dup {
		rq.logger.Infof("Quota operation satisfied through deduplication: dedupID %v, amount %v", args.DeduplicationID, result.amount)
		amount = result.amount
		exp = result.exp.Sub(currentTime)
		if exp < 0 {
			exp = 0
		}
	} else {
		amount, t, exp = qf(d, key, currentTime, currentTick)
		rq.recentDedup[args.DeduplicationID] = dedupState{amount: amount, exp: t}
	}

	rq.Unlock()

	return amount, exp, nil
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
