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
	"time"

	"istio.io/mixer/adapter/memQuota/util"
	"istio.io/mixer/adapter/redisquota/config"
	"istio.io/mixer/pkg/adapter"
)

type builder struct{ adapter.DefaultBuilder }

type redisQuota struct {
	// common info among different quota adapters
	common util.QuotaUtil

	// connection pool with redis
	redisPool *connPool
}

var (
	name = "redisQuota"
	desc = "Redis-based quotas."
	conf = &config.Params{
		MinDeduplicationDuration: time.Duration(1) * time.Second,
		RedisServerUrl:           "localhost:6379",
		SocketType:               "tcp",
		ConnectionPoolSize:       10,
	}
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

	if c.MinDeduplicationDuration <= 0 {
		ce = ce.Appendf("MinDeduplicationDuration", "deduplication window of %v is invalid, must be > 0", c.MinDeduplicationDuration)
	}

	if c.ConnectionPoolSize < 0 {
		ce = ce.Appendf("ConnectionPoolSize", "redis connection pool size of %v is invalid, must be > 0", c.ConnectionPoolSize)

	}

	return
}

func (builder) NewQuotasAspect(env adapter.Env, c adapter.Config, d map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
	return newAspect(env, c.(*config.Params))
}

// newAspect returns a new aspect.
func newAspect(env adapter.Env, c *config.Params) (adapter.QuotasAspect, error) {
	return newAspectWithDedup(env, time.NewTicker(c.MinDeduplicationDuration), c)
}

// newAspectWithDedup returns a new aspect.
func newAspectWithDedup(env adapter.Env, ticker *time.Ticker, c *config.Params) (adapter.QuotasAspect, error) {
	connPool, err := newConnPool(c.RedisServerUrl, c.SocketType, c.ConnectionPoolSize)
	if err != nil {
		// TODO: propagate Connection Pool error here
		return nil, err
	}
	rq := &redisQuota{
		common: util.QuotaUtil{
			RecentDedup: make(map[string]util.DedupState),
			OldDedup:    make(map[string]util.DedupState),
			Ticker:      ticker,
			GetTime:     time.Now,
			Logger:      env.Logger(),
		},
		redisPool: connPool,
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
	amount, exp, err := rq.common.CommonWrapper(args, func(d *adapter.QuotaDefinition, key string, currentTime time.Time, currentTick int64) (int64, time.Time,
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
			conn.pipeAppend("SET", key, d.MaxAmount)
			resp, _ = conn.pipeResponse()
			if resp.int() != d.MaxAmount {
				rq.common.Logger.Warningf("Could not set value to key.")
			}
		}

		return result, currentTime.Add(d.Expiration), d.Expiration
	})

	return adapter.QuotaResult{
		Amount:     amount,
		Expiration: exp,
	}, err

}

func (rq *redisQuota) ReleaseBestEffort(args adapter.QuotaArgs) (int64, error) {
	amount, _, err := rq.common.CommonWrapper(args,
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
			// if err != nil {
			// 	return 0, time.Time{}, 0
			// }
			ret := resp.int()

			if ret <= 0 {
				// delete the key since it contains no useful state
				conn.pipeAppend("DEL", key)
				// consume the output of previous command
				resp, _ = conn.pipeResponse()
				result += ret
			}

			return result, time.Time{}, 0
		})

	return amount, err
}

func (rq *redisQuota) Close() error {
	rq.redisPool.empty()
	return nil
}
