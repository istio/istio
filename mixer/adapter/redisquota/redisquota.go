// Copyright 2017 Istio Authors.
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
	"strconv"
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

	redisError error

	// rate-limiting algorithm: either fixed-window or rolling-window.
	rateLimit string
}

var (
	name = "redisQuota"
	desc = "Redis-based quotas."
	conf = &config.Params{
		MinDeduplicationDuration: 1 * time.Second,
		RedisServerUrl:           "localhost:6379",
		SocketType:               "tcp",
		ConnectionPoolSize:       10,
		RateLimitAlgorithm:       "fixed-window",
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

	if c.ConnectionPoolSize < 0 {
		ce = ce.Appendf("connectionPoolSize", "redis connection pool size of %v is invalid, must be > 0", c.ConnectionPoolSize)

	}

	if c.RateLimitAlgorithm != "rolling-window" && c.RateLimitAlgorithm != "fixed-window" {
		ce = ce.Appendf("rateLimitAlgorithm", "rate limit algorithm of %v is invalid, only fixed-window and rolling-window are supported", c.RateLimitAlgorithm)
	}

	return
}

func (builder) NewQuotasAspect(env adapter.Env, c adapter.Config, _ map[string]*adapter.QuotaDefinition) (adapter.QuotasAspect, error) {
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
		err = env.Logger().Errorf("Could not create connection pool with redis: %v", err)
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
		redisPool:  connPool,
		redisError: nil,
		rateLimit:  c.RateLimitAlgorithm,
	}

	env.ScheduleDaemon(func() {
		for range rq.common.Ticker.C {
			rq.common.Lock()
			rq.common.ReapDedup()
			rq.common.Unlock()
		}
	})
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
		seconds := int32((d.Expiration + time.Second - 1) / time.Second)

		conn, err := rq.redisPool.get()
		if err != nil {
			rq.redisError = rq.common.Logger.Errorf("Could not get connection to redis %v", err)
			return 0, time.Time{}, 0
		}
		defer rq.redisPool.put(conn)

		// Rolling-window algorithm
		// For rate-limiting use only, and no release for now.
		if rq.rateLimit == "rolling-window" {
			// Subsequent commands between MULTI and EXEC will be queued for atomic execution.
			// The queued commands will not produce any real response ('QUEUED' is the response here) until EXEC.
			conn.pipeAppend("MULTI")
			if _, err := conn.pipeResponse(); err != nil {
				rq.common.Logger.Warningf("Could not push commands into execution queue: %v", err)
				return 0, time.Time{}, 0
			}

			// Remove members in the sorted set which are out of the current window
			exp := currentTime.Add(-d.Expiration)
			conn.pipeAppend("ZREMRANGEBYSCORE", key, 0, strconv.Itoa(int(exp.UnixNano())))
			if _, err := conn.pipeResponse(); err != nil {
				rq.common.Logger.Warningf("Could not push commands into execution queue: %v", err)
				return 0, time.Time{}, 0
			}

			// Add the current request in the format of (member, score) to the sorted set, use timestamp as score here.
			now := int(currentTime.UnixNano())
			timestamp := strconv.Itoa(now)
			for i := 0; i < int(result); i++ {
				conn.pipeAppend("ZADD", key, timestamp, strconv.Itoa(now+i))
				if _, err := conn.pipeResponse(); err != nil {
					rq.common.Logger.Warningf("Could not push commands into execution queue: %v", err)
					return 0, time.Time{}, 0
				}
			}

			// Get the cardinality of the sorted set with key.
			conn.pipeAppend("ZCARD", key)
			if _, err := conn.pipeResponse(); err != nil {
				rq.common.Logger.Warningf("Could not push commands into execution queue: %v", err)
				return 0, time.Time{}, 0
			}

			// Atomic execution of all the previous commands, starting from "MULTI"
			conn.pipeAppend("EXEC")
			// This will be the actual response to EXEC.
			resp, err := conn.pipeResponse()
			if err != nil {
				rq.common.Logger.Warningf("Could not execute command on redis for expiring keys: %v", err)
				return 0, time.Time{}, 0
			}

			// The responses of previous commands are in an Array.
			info, err := resp.response.Array()
			if err != nil {
				rq.common.Logger.Warningf("Could not get array response after execution: %v", err)
				return 0, time.Time{}, 0
			}
			var lastElem int
			for i := range info {
				res, _ := info[i].Int()
				if i == len(info)-1 {
					lastElem = res
				}
			}

			if int64(lastElem) > d.MaxAmount {
				// Remove over added elements in sorted set.
				conn.pipeAppend("ZREMRANGEBYRANK", key, -int64(lastElem)+d.MaxAmount, -1)
				_, err := conn.getIntResp()
				if err != nil {
					rq.common.Logger.Warningf("Could not remove element in sorted set on redis for expiring keys: %v", err)
					return 0, time.Time{}, 0
				}

				if !bestEffort {
					return 0, time.Time{}, 0
				}
				// grab as much as we can
				result = d.MaxAmount - (int64(lastElem) - result)
			}
		}

		// Fixed-window algorithm
		// For quota enforcement.
		if rq.rateLimit == "fixed-window" {
			// increase the value of this key by the amount of result
			conn.pipeAppend("INCRBY", key, result)
			ret, err := conn.getIntResp()
			if err != nil {
				rq.redisError = rq.common.Logger.Errorf("Unable to get integer response from redis: %v", err)
				return 0, time.Time{}, 0
			}

			if d.Expiration != 0 {
				conn.pipeAppend("EXPIRE", key, seconds)
				rv, errInt := conn.getIntResp()
				if errInt != nil {
					rq.redisError = rq.common.Logger.Errorf("Got error when setting expire for key: %v", errInt)
					return 0, time.Time{}, 0
				} else if rv != 1 {
					rq.redisError = rq.common.Logger.Errorf("Could not set expire for key.")
					return 0, time.Time{}, 0
				}
			}

			if ret > d.MaxAmount {
				if !bestEffort {
					return 0, time.Time{}, 0
				}
				// grab as much as we can
				result = d.MaxAmount - (ret - result)
				conn.pipeAppend("DECRBY", key, (ret - d.MaxAmount))
				res, err := conn.getIntResp()
				if err != nil {
					rq.redisError = rq.common.Logger.Errorf("Unable to get integer response from redis: %v", err)
					return 0, time.Time{}, 0
				}
				if res != d.MaxAmount {
					rq.redisError = rq.common.Logger.Errorf("Could not set value to key.")
					return 0, time.Time{}, 0
				}
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
			seconds := int32((d.Expiration + time.Second - 1) / time.Second)

			conn, err := rq.redisPool.get()
			if err != nil {
				rq.redisError = rq.common.Logger.Errorf("Could not get connection to redis: %v", err)
				return 0, time.Time{}, 0
			}
			defer rq.redisPool.put(conn)

			conn.pipeAppend("GET", key)
			inuse, err := conn.getIntResp()
			if err != nil {
				rq.redisError = rq.common.Logger.Errorf("Unable to get integer response from redis: %v", err)
				return 0, time.Time{}, 0
			}

			// decrease the value of this key by the amount of result
			conn.pipeAppend("DECRBY", key, result)
			ret, err := conn.getIntResp()
			if err != nil {
				rq.redisError = rq.common.Logger.Errorf("Unable to get integer response from redis: %v", err)
				return 0, time.Time{}, 0
			}

			if d.Expiration != 0 {
				conn.pipeAppend("EXPIRE", key, seconds)
				rv, errInt := conn.getIntResp()
				if errInt != nil {
					rq.redisError = rq.common.Logger.Errorf("Got error when setting expire for key %v", errInt)
					return 0, time.Time{}, 0
				} else if rv != 1 {
					rq.redisError = rq.common.Logger.Errorf("Could not set expire for key")
					return 0, time.Time{}, 0
				}
			}

			if ret <= 0 {
				// delete the key since it contains no useful state
				conn.pipeAppend("DEL", key)
				// consume the output of previous command
				resp, err := conn.getIntResp()
				if err != nil {
					rq.redisError = rq.common.Logger.Errorf("Could not get response from redis %v", err)
					return 0, time.Time{}, 0
				} else if resp != 1 {
					rq.redisError = rq.common.Logger.Errorf("Could not remove key from redis")
					return 0, time.Time{}, 0
				}
				result = d.MaxAmount - inuse

			}

			return result, time.Time{}, 0
		})

	return amount, err
}

func (rq *redisQuota) Close() error {
	rq.redisPool.empty()
	return nil
}
